package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	openairt "github.com/WqyJh/go-openai-realtime/v2"
	"github.com/gen2brain/malgo"

	"github.com/richiejp/VoxInput/internal/audio"
	"github.com/richiejp/VoxInput/internal/localvqe"
	"github.com/richiejp/VoxInput/internal/gui"
	"github.com/richiejp/VoxInput/internal/input"
	"github.com/richiejp/VoxInput/internal/ipc"
	"github.com/richiejp/VoxInput/internal/pid"
)

// playbackJitterMs is the pre-roll the playback path requires before unblocking.
// TTS deltas arrive in bursts and the audio callback fires on a hard 20 ms
// cadence, so without a small playout buffer the speaker zero-fills (audible
// chop) any time a callback beats the next chunk. Refills on underrun.
const playbackJitterMs = 80

type ipcLogWriter struct {
	server *ipc.Server
}

func (w *ipcLogWriter) Write(p []byte) (int, error) {
	text := strings.TrimRight(string(p), "\n")
	if text == "" {
		return len(p), nil
	}
	w.server.Broadcast(ipc.Event{
		Kind: ipc.EventLog,
		Ts:   time.Now().UnixMilli(),
		Text: text,
	})
	return len(p), nil
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

// AECRefSource selects which signal drives the AEC reference channel.
type AECRefSource string

const (
	AECRefPlayback AECRefSource = "playback"
	AECRefMonitor  AECRefSource = "monitor"
)

type ListenConfig struct {
	PIDPath           string
	APIKey            string
	HTTPAPIBase       string
	WSAPIBase         string
	Lang              string
	Model             string
	Timeout           time.Duration
	UI                gui.StatusSink
	CaptureDevice     string
	OutputFile        string
	Prompt            string
	Mode              string
	AssistantModel    string
	AssistantVoice    string
	Instructions      string
	EnableDotool      bool
	InputController   input.Controller
	ScreenshotCommand string
	ScreenshotFile    string
	InputSampleRate   int
	OutputSampleRate  int
	EnableAEC         bool
	LocalVQEModelPath string
	LocalVQELibPath   string
	AECRefSource      AECRefSource
	AECMonitorDevice  string
	AECNoiseGate      bool
	AECNoiseGateDBFS  float32
	RefRing           *audio.Int16Ring
	DumpAudioDir      string
	IPCServer         *ipc.Server
}

type Listener struct {
	ctx               context.Context
	cancel            context.CancelFunc
	conn              *openairt.Conn
	errCh             chan error
	audioChunks       chan *bytes.Buffer
	chunkWriter       *audio.ChunkWriter
	config            ListenConfig
	streamConfig      audio.StreamConfig
	rtCli             *openairt.Client
	statePath         string
	audioPlayChunks   chan *bytes.Buffer
	playReader        *audio.ChunkReader
	processor         audio.AudioProcessor
	duplexOpts        *audio.DuplexOpts
	aecMicRing        *audio.Int16Ring
	aecRefRing        *audio.Int16Ring
	aecDumpProcessed  io.Writer
}

func NewListener(config ListenConfig, streamConfig audio.StreamConfig, rtCli *openairt.Client, statePath string, processor audio.AudioProcessor) *Listener {
	ctx, cancel := context.WithCancel(context.Background())
	l := &Listener{
		ctx:          ctx,
		cancel:       cancel,
		config:       config,
		streamConfig: streamConfig,
		rtCli:        rtCli,
		statePath:    statePath,
		errCh:        make(chan error, 1),
		audioChunks:  make(chan *bytes.Buffer, 1024),
		processor:    processor,
	}
	l.chunkWriter = audio.NewChunkWriter(l.ctx, l.audioChunks)
	l.audioPlayChunks = make(chan *bytes.Buffer, 1024)
	playbackRate := streamConfig.OutputSampleRate
	if playbackRate == 0 {
		playbackRate = streamConfig.SampleRate
	}
	prerollBytes := playbackRate * playbackJitterMs / 1000 * 2
	l.playReader = audio.NewChunkReader(l.ctx, l.audioPlayChunks, prerollBytes)

	monitorMode := config.RefRing != nil && config.AECRefSource == AECRefMonitor

	if config.DumpAudioDir != "" && config.Mode == "assistant" {
		if err := os.MkdirAll(config.DumpAudioDir, 0o755); err != nil {
			log.Printf("NewListener: failed to create dump dir: %v", err)
		} else {
			micFile, err := os.Create(filepath.Join(config.DumpAudioDir, "mic.raw"))
			if err != nil {
				log.Printf("NewListener: failed to create mic dump: %v", err)
			}
			spkFile, err := os.Create(filepath.Join(config.DumpAudioDir, "spk.raw"))
			if err != nil {
				log.Printf("NewListener: failed to create spk dump: %v", err)
			}
			aecFile, err := os.Create(filepath.Join(config.DumpAudioDir, "aec.raw"))
			if err != nil {
				log.Printf("NewListener: failed to create aec dump: %v", err)
			}
			var ttsFile *os.File
			if monitorMode {
				ttsFile, err = os.Create(filepath.Join(config.DumpAudioDir, "tts.raw"))
				if err != nil {
					log.Printf("NewListener: failed to create tts dump: %v", err)
				}
			}
			if micFile != nil && spkFile != nil && aecFile != nil {
				l.duplexOpts = &audio.DuplexOpts{
					DumpInput:  micFile,
					DumpOutput: spkFile,
				}
				// aecFile is written from the AEC worker when it runs
				// off-callback (processor != nil); the legacy inline path
				// falls back to DumpProcessed on duplexOpts.
				if processor != nil {
					l.aecDumpProcessed = aecFile
				} else {
					l.duplexOpts.DumpProcessed = aecFile
				}
				if ttsFile != nil {
					l.duplexOpts.DumpTTS = ttsFile
				}
				refSourceLabel := string(AECRefPlayback)
				if monitorMode {
					refSourceLabel = string(AECRefMonitor)
				}
				meta := map[string]any{
					"sampleRate":     streamConfig.SampleRate,
					"channels":       1,
					"format":         "s16le",
					"aec_ref_source": refSourceLabel,
				}
				metaPath := filepath.Join(config.DumpAudioDir, "meta.json")
				if metaBytes, err := json.Marshal(meta); err == nil {
					if err := os.WriteFile(metaPath, metaBytes, 0o644); err != nil {
						log.Printf("NewListener: failed to write meta.json: %v", err)
					}
				}
				log.Printf("NewListener: dumping audio to %s", config.DumpAudioDir)
			}
		}
	}

	if monitorMode {
		if l.duplexOpts == nil {
			l.duplexOpts = &audio.DuplexOpts{}
		}
		l.duplexOpts.RefSource = config.RefRing
	}

	// When a processor is configured, route AEC work through a dedicated
	// worker goroutine so inference never blocks the realtime audio callback.
	if processor != nil {
		l.aecMicRing = audio.NewInt16Ring(streamConfig.SampleRate) // ~1s buffer
		l.aecRefRing = audio.NewInt16Ring(streamConfig.SampleRate)
		if l.duplexOpts == nil {
			l.duplexOpts = &audio.DuplexOpts{}
		}
		l.duplexOpts.AECMicRing = l.aecMicRing
		l.duplexOpts.AECRefRing = l.aecRefRing
	}

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
	l.config.UI.Send(&gui.ShowListeningMsg{})

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
	l.chunkWriter.Flush()
	l.conn.Close()
	l.cancel()
	if l.duplexOpts != nil {
		if c, ok := l.duplexOpts.DumpInput.(io.Closer); ok {
			c.Close()
		}
		if c, ok := l.duplexOpts.DumpOutput.(io.Closer); ok {
			c.Close()
		}
		if c, ok := l.duplexOpts.DumpProcessed.(io.Closer); ok {
			c.Close()
		}
		if c, ok := l.duplexOpts.DumpTTS.(io.Closer); ok {
			c.Close()
		}
	}
	if err := pid.WriteState(l.statePath, false); err != nil {
		log.Println("Listener.Stop: failed to write idle state: ", err)
	}
}

func listen(config ListenConfig) {
	if config.IPCServer != nil {
		lw := io.MultiWriter(os.Stderr, &ipcLogWriter{server: config.IPCServer})
		log.SetOutput(lw)
	}

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

	// In assistant mode with duplex audio, use the higher of input/output sample rates
	// Downsampling will be handled in the audio package
	// In transcription mode, use InputSampleRate for capture only
	sampleRate := config.InputSampleRate
	if config.Mode == "assistant" {
		if config.OutputSampleRate > config.InputSampleRate {
			sampleRate = config.OutputSampleRate
		}
	}

	periodMs := 20
	streamConfig := audio.StreamConfig{
		Format:           malgo.FormatS16,
		Channels:         1,
		SampleRate:       sampleRate,
		InputSampleRate:  config.InputSampleRate,
		OutputSampleRate: config.OutputSampleRate,
		MalgoContext:     mctx.Context,
		PeriodMs:         periodMs,
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

	// Upper bound on bytes per Process call: 2× period at deviceRate × 2 bytes/sample.
	// Matches the Duplex callback's own preallocation so the processor never
	// needs to grow its scratch at runtime.
	maxProcessBytes := 2 * periodMs * sampleRate / 1000 * 2

	var processor audio.AudioProcessor
	if config.EnableAEC && config.Mode == "assistant" {
		modelPath, err := localvqe.EnsureModel(config.LocalVQEModelPath)
		if err != nil {
			log.Fatalf("listen: failed to ensure localvqe model: %v", err)
		}
		libPath, err := localvqe.EnsureLib(config.LocalVQELibPath)
		if err != nil {
			log.Fatalf("listen: failed to find localvqe lib: %v", err)
		}
		engine, err := localvqe.New(libPath, modelPath)
		if err != nil {
			log.Fatalf("listen: failed to create localvqe engine: %v", err)
		}
		if config.AECNoiseGate {
			if err := engine.SetNoiseGate(true, config.AECNoiseGateDBFS); err != nil {
				log.Fatalf("listen: failed to enable noise gate: %v", err)
			}
			log.Printf("listen: LocalVQE noise gate enabled (threshold=%.1f dBFS)", config.AECNoiseGateDBFS)
		}
		processor = audio.NewLocalVQEProcessor(engine, sampleRate, maxProcessBytes)
		log.Printf("listen: LocalVQE AEC enabled (modelRate=%d, deviceRate=%d, hopLength=%d, refSource=%s, maxProcessBytes=%d)",
			engine.SampleRate(), sampleRate, engine.HopLength(), config.AECRefSource, maxProcessBytes)
	}

	if processor != nil && config.AECRefSource == AECRefMonitor {
		if config.AECMonitorDevice == "" {
			log.Fatalln("listen: VOXINPUT_AEC_REF_SOURCE=monitor requires VOXINPUT_AEC_MONITOR_DEVICE (or --aec-monitor-device)")
		}
		monitorConfig := audio.StreamConfig{
			Format:       malgo.FormatS16,
			Channels:     1,
			SampleRate:   sampleRate,
			MalgoContext: mctx.Context,
			PeriodMs:     periodMs,
		}
		found, err := monitorConfig.SetCaptureDeviceByName(&mctx.Context, config.AECMonitorDevice)
		if err != nil {
			log.Fatalln("listen: failed to query monitor capture devices: ", err)
		}
		if !found {
			log.Fatalf("listen: monitor device not found: %s\nRun 'voxinput devices' to list available capture devices.", config.AECMonitorDevice)
		}
		config.RefRing = audio.NewInt16Ring(sampleRate) // ~1s buffer
		monitorCtx, cancelMonitor := context.WithCancel(context.Background())
		defer cancelMonitor()
		go func() {
			if err := audio.CaptureToRing(monitorCtx, config.RefRing, monitorConfig); err != nil &&
				!errors.Is(err, context.Canceled) {
				log.Printf("listen: monitor capture ended: %v", err)
			}
		}()
		log.Printf("listen: AEC monitor capture started (device=%q, rate=%d)", config.AECMonitorDevice, sampleRate)
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

	var ipcCmds <-chan ipc.Command
	if config.IPCServer != nil {
		ipcCmds = config.IPCServer.Commands()
	}

ForListen:
	for {
		log.Println("listen: Waiting for record signal...")

		var sig os.Signal
		for {
			select {
			case sig = <-sigChan:
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
				continue
			case cmd := <-ipcCmds:
				switch cmd.Kind {
				case ipc.CommandRecord:
					// start
				case ipc.CommandStop:
					log.Println("listen: Received IPC stop, but wasn't recording")
					continue
				case ipc.CommandQuit:
					break ForListen
				default:
					continue
				}
			}
			break
		}

		l := NewListener(config, streamConfig, rtCli, statePath, processor)
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
					break ForSignal
				case syscall.SIGTERM:
					l.config.UI.Send(&gui.ShowStoppingMsg{})
					l.Stop()
					break ForListen
				}
			case cmd := <-ipcCmds:
				switch cmd.Kind {
				case ipc.CommandRecord:
					log.Println("listen: received IPC record, but already recording")
				case ipc.CommandStop:
					break ForSignal
				case ipc.CommandQuit:
					l.config.UI.Send(&gui.ShowStoppingMsg{})
					l.Stop()
					break ForListen
				}
			case <-l.ctx.Done():
				break ForSignal
			}
		}

		l.config.UI.Send(&gui.ShowStoppingMsg{})
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
