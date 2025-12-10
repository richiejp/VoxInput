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
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	openairt "github.com/WqyJh/go-openai-realtime"
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
	if now.Sub(rbw.lastSend) >= 500*time.Millisecond {
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
				log.Println("main: Connection failed: ", err)
				return err
			}
			log.Println("main: error receiving message, retrying: ", err)
			time.Sleep(250 * time.Millisecond)
			continue
		}

		log.Println("main: received message of type: ", msg.ServerEventType())

		switch msg.ServerEventType() {
		case openairt.ServerEventTypeError:
			log.Println("main: Server error: ", msg.(openairt.ErrorEvent).Error.Message)
		case openairt.ServerEventTypeConversationCreated:
		case openairt.ServerEventTypeSessionCreated:
			fallthrough
		case openairt.ServerEventTypeTranscriptionSessionCreated:
			fallthrough
		case openairt.ServerEventTypeTranscriptionSessionUpdated:
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
	PIDPath string
	APIKey string
	HTTPAPIBase string
	WSAPIBase string
	Lang string
	Model string
	Timeout time.Duration
	UI *gui.GUI
	CaptureDevice string
}

func listen(config ListenConfig) {
	mctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
		log.Print("internal/audio: ", message)
	})
	if err != nil {
		log.Fatalln("main: ", err)
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
		log.Fatalln("main: failed to get state file path: ", err)
	}

	err = pid.Write(config.PIDPath)
	defer func() {
		if err := os.Remove(config.PIDPath); err != nil {
			log.Println("main: failed to remove PID file: ", err)
		}
		if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
			log.Println("main: failed to remove state file: ", err)
		}
	}()

	if err := pid.WriteState(statePath, false); err != nil {
		log.Println("main: failed to write initial state: ", err)
	}

Listen:
	for {
		log.Println("main: Waiting for record signal...")

		ctx, cancel := context.WithCancel(context.Background())
		for sig := range sigChan {
			switch sig {
			case syscall.SIGUSR1:
			case syscall.SIGUSR2:
				log.Println("main: Received stop/write signal, but wasn't recording")
				continue
			case syscall.SIGTERM:
				cancel()
				break Listen
			}
			break
		}

		initCtx, finishInit := context.WithTimeout(ctx, config.Timeout)
		errCh := make(chan error, 1)
		conn, err := rtCli.Connect(initCtx, openairt.WithIntent())
		if err != nil {
			log.Println("main: realtime connect: ", err)
			finishInit()
			cancel()
			continue
		}
		log.Println("main: Connected to realtime API, waiting for session.created event...")

		// It's not required to wait for this, but the server may take time to startup
		if err := waitForSessionUpdated(initCtx, conn); err != nil {
			finishInit()
			cancel()
			break Listen
		}

		err = conn.SendMessage(initCtx, openairt.TranscriptionSessionUpdateEvent{
			EventBase: openairt.EventBase{
				EventID: "Initial update",
			},
			Session: openairt.ClientTranscriptionSession{
				InputAudioTranscription: &openairt.InputAudioTranscription{
					Model:    config.Model,
					Language: config.Lang,
				},
				TurnDetection: &openairt.ClientTurnDetection{
					Type: openairt.ClientTurnDetectionTypeServerVad,
				},
			},
		})

		if err := waitForSessionUpdated(initCtx, conn); err != nil {
			finishInit()
			cancel()
			break Listen
		}

		finishInit()
		log.Println("main: Record/Transcribe...")

		if err := pid.WriteState(statePath, true); err != nil {
			log.Println("main: failed to write recording state: ", err)
		}

		config.UI.Chan <- &gui.ShowListeningMsg{}

		audioChunks := make(chan (*bytes.Buffer), 10)
		chunkWriter := newChunkWriter(ctx, audioChunks)

		go func() {
			if err := audio.Capture(ctx, chunkWriter, streamConfig); err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				errCh <- fmt.Errorf("audio capture: %w", err)
				cancel()
			}
		}()

		go func() {
			var headerBuf bytes.Buffer

			wavHeader := audio.NewWAVHeader(0)

			for {
				headerBuf.Reset()

				var cur *bytes.Buffer
				select {
				// Received from chunkWriter
				case cur = <-audioChunks:
				case <-ctx.Done():
					return
				}

				log.Printf("main: transcribing, %d\n", cur.Len())

				wavHeader.ChunkSize = uint32(cur.Len())
				if err := wavHeader.Write(&headerBuf); err != nil {
					errCh <- fmt.Errorf("write wav header: %w", err)
					cancel()
					return
				}

				if err := conn.SendMessage(ctx, openairt.InputAudioBufferAppendEvent{
					EventBase: openairt.EventBase{
						EventID: "TODO",
					},
					Audio: base64.StdEncoding.EncodeToString(cur.Bytes()),
				}); err != nil {
					var permanent *openairt.PermanentError
					if errors.As(err, &permanent) {
						errCh <- fmt.Errorf("main: connection failed: %w", err)
						cancel()
						break
					}
					log.Println("main: error sending message: ", err)
					continue
				}
			}
		}()

		go func() {
			for {
				msg, err := conn.ReadMessage(ctx)
				if err != nil {
					var permanent *openairt.PermanentError
					if errors.As(err, &permanent) {
						log.Println("main: Connection failed: ", err)
						cancel()
						break
					}
					log.Println("main: error receiving message, retrying: ", err)
					continue
				}

				log.Println("main: receiving message: ", msg.ServerEventType())

				var text string
				switch msg.ServerEventType() {
				case openairt.ServerEventTypeInputAudioBufferSpeechStarted:
					config.UI.Chan <- &gui.ShowSpeechDetectedMsg{}
				case openairt.ServerEventTypeInputAudioBufferSpeechStopped:
					config.UI.Chan <- &gui.ShowTranscribingMsg{}
				case openairt.ServerEventTypeResponseAudioTranscriptDone:
					text = msg.(openairt.ResponseAudioTranscriptDoneEvent).Transcript
				case openairt.ServerEventTypeConversationItemInputAudioTranscriptionCompleted:
					text = msg.(openairt.ConversationItemInputAudioTranscriptionCompletedEvent).Transcript
				case openairt.ServerEventTypeError:
					log.Println("main: server error: ", msg.(openairt.ErrorEvent).Error.Message)
					continue
				default:
					continue
				}

				if text == "" {
					continue
				}

				config.UI.Chan <- &gui.HideMsg{}

				log.Println("main: received transcribed text: ", text)

				dotool := exec.CommandContext(ctx, "dotool")
				stdin, err := dotool.StdinPipe()
				if err != nil {
					errCh <- fmt.Errorf("dotool stdin pipe: %w", err)
					cancel()
					return
				}
				dotool.Stderr = os.Stderr

				if err := dotool.Start(); err != nil {
					errCh <- fmt.Errorf("dotool stderr pipe: %w", err)
					cancel()
					return
				}

				_, err = io.WriteString(stdin, fmt.Sprintf("type %s ", text))
				if err != nil {
					errCh <- fmt.Errorf("dotool stdin WriteString: %w", err)
					cancel()
					return
				}

				if err := stdin.Close(); err != nil {
					errCh <- fmt.Errorf("close dotool stdin: %w", err)
					cancel()
					return
				}

				if err := dotool.Wait(); err != nil {
					if errors.Is(err, context.Canceled) {
						return
					}
					errCh <- fmt.Errorf("dotool wait: %w", err)
					cancel()
					return
				}
			}
		}()

		for {
			select {
			case <-ctx.Done():
			case sig := <-sigChan:
				switch sig {
				case syscall.SIGUSR1:
					log.Println("main: received record signal, but already recording")
					continue
				case syscall.SIGUSR2:
					// TODO: Do input_audio_buffer.commit and/or wait for final transcription?
				case syscall.SIGTERM:
					conn.Close()
					cancel()
					break Listen
				}
			}

			break
		}

		config.UI.Chan <- &gui.ShowStoppingMsg{}
		log.Println("main: finished transcribing")
		conn.Close()
		cancel()

		// Set state back to idle
		if err := pid.WriteState(statePath, false); err != nil {
			log.Println("main: failed to write idle state: ", err)
		}

		for {
			select {
			case err := <-errCh:
				if err != nil && !errors.Is(err, context.Canceled) {
					log.Fatalln("main: ", err)
				}
			default:
				continue Listen
			}
		}
	}
}
