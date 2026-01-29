package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	openairt "github.com/WqyJh/go-openai-realtime/v2"
	"github.com/gen2brain/malgo"

	"github.com/richiejp/VoxInput/internal/audio"
	"github.com/richiejp/VoxInput/internal/gui"
	"github.com/richiejp/VoxInput/internal/pid"
)

type chunkWriter struct {
	ctx      context.Context
	ready    chan<- (*bytes.Buffer)
	current  *bytes.Buffer
	lastSend time.Time
}

func newChunkWriter(ctx context.Context, ready chan<- (*bytes.Buffer)) *chunkWriter {
	return &chunkWriter{
		ctx:      ctx,
		ready:    ready,
		current:  new(bytes.Buffer),
		lastSend: time.Now(),
	}
}

func (rbw *chunkWriter) Write(p []byte) (n int, err error) {
	now := time.Now()
	if now.Sub(rbw.lastSend) >= 250*time.Millisecond {
		select {
		case rbw.ready <- rbw.current:
			break
		case <-rbw.ctx.Done():
			return 0, rbw.ctx.Err()
		}
		rbw.current = new(bytes.Buffer)
		rbw.lastSend = now
	}

	return rbw.current.Write(p)
}

type chunkReader struct {
	ctx     context.Context
	chunks  <-chan *bytes.Buffer
	current *bytes.Buffer
}

func newChunkReader(ctx context.Context, chunks <-chan *bytes.Buffer) *chunkReader {
	return &chunkReader{
		ctx:    ctx,
		chunks: chunks,
	}
}

func (cr *chunkReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	n := 0
	for len(p) > 0 {
		if cr.current == nil || cr.current.Len() == 0 {
			select {
			case buf := <-cr.chunks:
				cr.current = buf
			default:
				return n, nil
			}
		}
		if cr.current == nil {
			return n, nil
		}

		nn, err := cr.current.Read(p)
		n += nn
		p = p[nn:]
		if err == io.EOF {
			cr.current = nil
			continue
		}
		return n, nil
	}
	return n, nil
}

func getPrefixedEnv(prefixes []string, name string, fallback string) (val string) {
	for _, p := range prefixes {
		var n string
		if p == "" {
			n = name
		} else {
			n = p + "_" + name
		}
		if val = os.Getenv(n); val != "" {
			return val
		}
	}

	return fallback
}

func getOpenaiEnv(name string, fallback string) string {
	return getPrefixedEnv([]string{"VOXINPUT", "OPENAI"}, name, fallback)
}

func waitForSessionUpdated(ctx context.Context, conn *openairt.Conn) error {
	for {
		msg, err := conn.ReadMessage(ctx)
		if err != nil {
			var permanent *openairt.PermanentError
			if errors.As(err, &permanent) {
				log.Println("waitForSessionUpdated: Connection failed: ", err)
				return err
			}
			log.Println("waitForSessionUpdated: error receiving message, retrying: ", err)
			time.Sleep(250 * time.Millisecond)
			continue
		}

		log.Println("waitForSessionUpdated: received message of type: ", msg.ServerEventType())

		switch msg.ServerEventType() {
		case openairt.ServerEventTypeError:
			log.Println("waitForSessionUpdated: Server error: ", msg.(openairt.ErrorEvent).Error.Message)
		case openairt.ServerEventTypeSessionCreated:
			fallthrough
		case openairt.ServerEventTypeSessionUpdated:
			return nil
		}

		select {
		case <-ctx.Done():
			return nil
		default:
		}
	}
}

type ListenConfig struct {
	PIDPath        string
	APIKey         string
	HTTPAPIBase    string
	WSAPIBase      string
	Lang           string
	Model          string
	Timeout        time.Duration
	UI             *gui.GUI
	CaptureDevice  string
	OutputFile     string
	Prompt         string
	Mode           string
	AssistantModel string
	AssistantVoice string
	Instructions   string
}

type Listener struct {
	ctx             context.Context
	cancel          context.CancelFunc
	conn            *openairt.Conn
	errCh           chan error
	audioChunks     chan *bytes.Buffer
	chunkWriter     *chunkWriter
	config          ListenConfig
	streamConfig    audio.StreamConfig
	rtCli           *openairt.Client
	statePath       string
	audioPlayChunks chan *bytes.Buffer
	playReader      *chunkReader
}

func NewListener(config ListenConfig, streamConfig audio.StreamConfig, rtCli *openairt.Client, statePath string) *Listener {
	ctx, cancel := context.WithCancel(context.Background())
	l := &Listener{
		ctx:          ctx,
		cancel:       cancel,
		config:       config,
		streamConfig: streamConfig,
		rtCli:        rtCli,
		statePath:    statePath,
		errCh:        make(chan error, 1),
		audioChunks:  make(chan *bytes.Buffer, 10),
	}
	l.chunkWriter = newChunkWriter(l.ctx, l.audioChunks)
	l.audioPlayChunks = make(chan *bytes.Buffer, 10)
	l.playReader = newChunkReader(l.ctx, l.audioPlayChunks)

	return l
}

func (l *Listener) Start() error {
	initCtx, finishInit := context.WithTimeout(l.ctx, l.config.Timeout)
	opts := []openairt.ConnectOption{}
	// The session always starts in assistant mode, so the user may need to specify a valid assistant model
	// even if they only use transcription. The default assistant model is gpt-realtime which may not be defined in LocalAI
	if l.config.AssistantModel != "" {
		opts = append(opts, openairt.WithModel(l.config.AssistantModel))
	}

	conn, err := l.rtCli.Connect(initCtx, opts...)
	if err != nil {
		log.Println("Listener.Start: realtime connect: ", err)
		finishInit()
		return err
	}
	l.conn = conn
	log.Println("Listener.Start: Connected to realtime API, waiting for session.created event...")
	if err := waitForSessionUpdated(initCtx, l.conn); err != nil {
		finishInit()
		return err
	}
	if l.config.Mode == "assistant" {
		err = l.startAssistantSession(initCtx)
	} else {
		err = l.startTranscriptionSession(initCtx)
	}
	if err != nil {
		log.Println("Listener.Start: error sending initial update: ", err)
		finishInit()
		return err
	}
	if err := waitForSessionUpdated(initCtx, l.conn); err != nil {
		finishInit()
		return err
	}
	finishInit()
	log.Println("Listener.Start: Record/Transcribe...")
	if err := pid.WriteState(l.statePath, true); err != nil {
		log.Println("Listener.Start: failed to write recording state: ", err)
	}
	l.config.UI.Chan <- &gui.ShowListeningMsg{}

	return nil
}

func (l *Listener) RunAudio() {
	if l.config.Mode == "assistant" {
		l.runAudioAssistant()
	} else {
		l.runAudioTranscription()
	}
}

func (l *Listener) SendChunks() {
	for {
		var cur *bytes.Buffer
		select {
		case cur = <-l.audioChunks:
		case <-l.ctx.Done():
			return
		}
		log.Printf("Listener.SendChunks: transcribing, %d\n", cur.Len())
		if cur.Len() < 1 {
			continue
		}
		if err := l.conn.SendMessage(l.ctx, openairt.InputAudioBufferAppendEvent{
			EventBase: openairt.EventBase{
				EventID: "TODO",
			},
			Audio: base64.StdEncoding.EncodeToString(cur.Bytes()),
		}); err != nil {
			var permanent *openairt.PermanentError
			if errors.As(err, &permanent) {
				l.errCh <- fmt.Errorf("Listener.SendChunks: connection failed: %w", err)
				l.cancel()
				return
			}
			log.Println("Listener.SendChunks: error sending message: ", err)
			continue
		}
	}
}

func (l *Listener) Stop() {
	log.Println("Listener.Stop: finished transcribing")
	l.conn.Close()
	l.cancel()
	if err := pid.WriteState(l.statePath, false); err != nil {
		log.Println("Listener.Stop: failed to write idle state: ", err)
	}
}

func listen(config ListenConfig) {
	mctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
		log.Print("internal/audio: ", message)
	})
	if err != nil {
		log.Fatalln("listen: ", err)
	}
	defer func() {
		_ = mctx.Uninit()
		mctx.Free()
	}()

	streamConfig := audio.StreamConfig{
		Format:       malgo.FormatS16,
		Channels:     1,
		SampleRate:   24000,
		MalgoContext: mctx.Context,
	}

	captureDeviceName := config.CaptureDevice
	if captureDeviceName != "" {
		found, err := streamConfig.SetCaptureDeviceByName(&mctx.Context, captureDeviceName)
		if err != nil {
			log.Fatalln("Failed to query devices:", err)
		}
		if !found {
			log.Fatalf("Capture device not found: %s\nRun 'voxinput devices' to list available devices.", captureDeviceName)
		}
		log.Printf("Using capture device: %s", captureDeviceName)
	}

	rtConf := openairt.DefaultConfig(config.APIKey)
	rtConf.BaseURL = config.WSAPIBase
	rtConf.APIBaseURL = config.HTTPAPIBase
	rtConf.HTTPClient = &http.Client{Timeout: config.Timeout}
	rtCli := openairt.NewClientWithConfig(rtConf)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGUSR1)
	signal.Notify(sigChan, syscall.SIGUSR2)
	signal.Notify(sigChan, syscall.SIGTERM)

	statePath, err := pid.StatePath()
	if err != nil {
		log.Fatalln("listen: failed to get state file path: ", err)
	}

	err = pid.Write(config.PIDPath)
	defer func() {
		if err := os.Remove(config.PIDPath); err != nil {
			log.Println("listen: failed to remove PID file: ", err)
		}
		if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
			log.Println("listen: failed to remove state file: ", err)
		}
	}()

	if err := pid.WriteState(statePath, false); err != nil {
		log.Println("listen: failed to write initial state: ", err)
	}

ForListen:
	for {
		log.Println("listen: Waiting for record signal...")

		var sig os.Signal
		for {
			sig = <-sigChan
			switch sig {
			case syscall.SIGUSR1:
				// start
			case syscall.SIGUSR2:
				log.Println("listen: Received stop/write signal, but wasn't recording")
				continue
			case syscall.SIGTERM:
				break ForListen
			}
			if sig == syscall.SIGUSR1 {
				break
			}
		}

		l := NewListener(config, streamConfig, rtCli, statePath)
		if err := l.Start(); err != nil {
			l.cancel()
			continue
		}

		go l.RunAudio()
		go l.SendChunks()
		if l.config.Mode == "assistant" {
			go l.ReceiveAssistantMessages()
		} else {
			go l.ReceiveTranscriptionMessages()
		}

	ForSignal:
		for {
			select {
			case sig = <-sigChan:
				switch sig {
				case syscall.SIGUSR1:
					log.Println("listen: received record signal, but already recording")
				case syscall.SIGUSR2:
					// TODO: Do input_audio_buffer.commit and/or wait for final transcription?
					break ForSignal
				case syscall.SIGTERM:
					l.config.UI.Chan <- &gui.ShowStoppingMsg{}
					l.Stop()
					break ForListen
				}
			// We check this incase there was an error. Otherwise we would sit here waiting
			// for a signal to stop listening when we have effectively already stopped listening
			case <-l.ctx.Done():
				break ForSignal
			}
		}

		l.config.UI.Chan <- &gui.ShowStoppingMsg{}
		l.Stop()

		for {
			select {
			case err := <-l.errCh:
				if err != nil && !errors.Is(err, context.Canceled) {
					log.Fatalln("listen: ", err)
				}
			default:
				continue ForListen
			}
		}
	}
}
