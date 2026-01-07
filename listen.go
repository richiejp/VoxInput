package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gen2brain/malgo"
	sherpa "github.com/k2-fsa/sherpa-onnx-go-linux"
	resampling "github.com/tphakala/go-audio-resampler"

	"github.com/richiejp/VoxInput/internal/audio"
	"github.com/richiejp/VoxInput/internal/gui"
	"github.com/richiejp/VoxInput/internal/pid"
)

const (
	CAPTURE_RATE   = 24000           // Input sample rate from microphone
	MODEL_RATE     = 16000           // Sample rate required by Sherpa model
	CAPTURE_FORMAT = malgo.FormatS16 // Audio capture format
)

type chunkWriter struct {
	ctx    context.Context
	ready  chan<- []float32
	format malgo.FormatType
}

func newChunkWriter(ctx context.Context, ready chan<- []float32, format malgo.FormatType) *chunkWriter {
	return &chunkWriter{
		ctx:    ctx,
		ready:  ready,
		format: format,
	}
}

func (cw *chunkWriter) Write(p []byte) (n int, err error) {
	samples := cw.bytesToFloat32(p)

	select {
	case cw.ready <- samples:
		return len(p), nil
	case <-cw.ctx.Done():
		return 0, cw.ctx.Err()
	default:
		// Channel full, drop samples with warning
		log.Println("chunkWriter: audioSamples channel full, dropping samples")
		return len(p), nil
	}
}

func (cw *chunkWriter) bytesToFloat32(inSamples []byte) []float32 {
	switch cw.format {
	case malgo.FormatS16:
		numSamples := len(inSamples) / 2
		outSamples := make([]float32, numSamples)
		for i := 0; i < numSamples; i++ {
			s16 := int16(inSamples[2*i]) | int16(inSamples[2*i+1])<<8
			outSamples[i] = float32(s16) / 32768.0
		}
		return outSamples
	default:
		log.Printf("chunkWriter: unsupported format %v, returning empty", cw.format)
		return []float32{}
	}
}

type TranscriptionUpdate struct {
	NewText    string
	IsEndpoint bool
}

// TextUpdate represents the changes needed to transform old text to new text
type TextUpdate struct {
	CommonPrefixLen int
	ToDelete        string
	DeleteCount     int
	ToAppend        string
}

// ComputeTextUpdate calculates the longest common prefix and determines
// what needs to be deleted and appended
func ComputeTextUpdate(oldText, newText string) TextUpdate {
	// Find longest common prefix
	lcpLen := 0
	minLen := len(oldText)
	if len(newText) < minLen {
		minLen = len(newText)
	}

	for lcpLen < minLen && oldText[lcpLen] == newText[lcpLen] {
		lcpLen++
	}

	// Calculate what needs to be deleted (remainder of old text)
	toDelete := oldText[lcpLen:]
	deleteCount := len(toDelete)

	// Calculate what needs to be appended
	toAppend := newText[lcpLen:]

	return TextUpdate{
		CommonPrefixLen: lcpLen,
		ToDelete:        toDelete,
		DeleteCount:     deleteCount,
		ToAppend:        toAppend,
	}
}

func (u TextUpdate) String() string {
	return fmt.Sprintf("prefix_len=%d, to_delete='%s' (backspaces=%d), to_append='%s'",
		u.CommonPrefixLen, u.ToDelete, u.DeleteCount, u.ToAppend)
}

func initRecognizer() *sherpa.OnlineRecognizer {
	config := sherpa.OnlineRecognizerConfig{}
	config.FeatConfig = sherpa.FeatureConfig{
		SampleRate: MODEL_RATE,
		FeatureDim: 80,
	}

	// Model paths - hardcoded for now
	// TODO: Make these configurable via environment variables or flags
	config.ModelConfig.Transducer.Encoder = "./sherpa-stt-onnx-en-kroko_128l/encoder.int8.onnx"
	config.ModelConfig.Transducer.Decoder = "./sherpa-stt-onnx-en-kroko_128l/decoder.int8.onnx"
	config.ModelConfig.Transducer.Joiner = "./sherpa-stt-onnx-en-kroko_128l/joiner.int8.onnx"
	config.ModelConfig.Paraformer.Encoder = ""
	config.ModelConfig.Paraformer.Decoder = ""
	config.ModelConfig.Tokens = "./sherpa-stt-onnx-en-kroko_128l/tokens.txt"
	config.ModelConfig.NumThreads = 1
	config.ModelConfig.Debug = 0
	config.ModelConfig.ModelType = "nemo_transducer"
	config.ModelConfig.Provider = "cpu"

	// Decoding configuration
	config.DecodingMethod = "greedy_search" // or modified_beam_search
	config.MaxActivePaths = 4

	// Endpoint detection configuration
	config.EnableEndpoint = 1
	config.Rule1MinTrailingSilence = 2.4
	config.Rule2MinTrailingSilence = 1.2
	config.Rule3MinUtteranceLength = 20

	log.Println("main: Initializing Sherpa recognizer (may take several seconds)...")
	recognizer := sherpa.NewOnlineRecognizer(&config)
	log.Println("main: Sherpa recognizer created!")

	return recognizer
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

type ListenConfig struct {
	PIDPath string
	UI      *gui.GUI
}

// TODO: Reimplment replay
func listen(config ListenConfig) {
	// Initialize malgo context
	mctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
		log.Print("internal/audio: ", message)
	})
	if err != nil {
		log.Fatalln("main: failed to initialize malgo context: ", err)
	}
	defer func() {
		_ = mctx.Uninit()
		mctx.Free()
	}()

	// Initialize Sherpa recognizer once at startup
	recognizer := initRecognizer()
	defer sherpa.DeleteOnlineRecognizer(recognizer)

	// Stream configuration - uses CAPTURE_RATE and CAPTURE_FORMAT
	streamConfig := audio.StreamConfig{
		Format:       CAPTURE_FORMAT,
		Channels:     1,
		SampleRate:   CAPTURE_RATE,
		MalgoContext: mctx.Context,
	}

	// Signal handling setup
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGUSR1)
	signal.Notify(sigChan, syscall.SIGUSR2)
	signal.Notify(sigChan, syscall.SIGTERM)

	// PID and state file management
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
		log.Println("listen: Waiting for record signal (SIGUSR1)...")
		ctx, cancel := context.WithCancel(context.Background())

		for sig := range sigChan {
			switch sig {
			case syscall.SIGUSR1:
				// Start recording
				break
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

		log.Println("main: Starting recording session...")
		// Create Sherpa stream for this recording session
		stream := sherpa.NewOnlineStream(recognizer)
		// Create resampler: CAPTURE_RATE -> MODEL_RATE
		resampler, err := resampling.New(&resampling.Config{
			InputRate:  CAPTURE_RATE,
			OutputRate: MODEL_RATE,
			Channels:   1,
			Quality:    resampling.QualitySpec{Preset: resampling.QualityHigh},
		})
		if err != nil {
			log.Println("main: failed to create resampler: ", err)
			sherpa.DeleteOnlineStream(stream)
			cancel()
			continue
		}

		// Update state
		if err := pid.WriteState(statePath, true); err != nil {
			log.Println("main: failed to write recording state: ", err)
		}
		config.UI.Chan <- &gui.ShowListeningMsg{}

		// Create channels
		errCh := make(chan error, 1)
		audioSamples := make(chan []float32, 600)     // Float32 at CAPTURE_RATE
		resampledAudio := make(chan []float32, 600)   // Float32 at MODEL_RATE
		results := make(chan TranscriptionUpdate, 10) // Transcribed text

		// 1. Audio Capture Goroutine
		// Captures audio, converts int16->float32, sends to audioSamples
		chunkWriter := newChunkWriter(ctx, audioSamples, CAPTURE_FORMAT)
		go func() {
			log.Println("main: Audio capture goroutine started")
			if err := audio.Capture(ctx, chunkWriter, streamConfig); err != nil {
				if errors.Is(err, context.Canceled) {
					log.Println("main: Audio capture goroutine stopped (context cancelled)")
					return
				}
				errCh <- fmt.Errorf("audio capture: %w", err)
				cancel()
			}
		}()

		// 2. Resampling Goroutine
		// Resamples from CAPTURE_RATE to MODEL_RATE
		go func() {
			defer close(resampledAudio)
			log.Println("main: Resampling goroutine started")

			for {
				select {
				case samples, ok := <-audioSamples:
					if !ok {
						// Flush remaining samples from resampler
						final64, err := resampler.Flush()
						if err != nil {
							log.Println("main: resampler flush error: ", err)
						}
						final := make([]float32, len(final64))
						for i, v := range final64 {
							final[i] = float32(v)
						}
						if len(final) > 0 {
							select {
							case resampledAudio <- final:
							case <-ctx.Done():
							}
						}
						log.Println("main: Resampling goroutine stopped (channel closed)")
						return
					}

					output, err := resampler.ProcessFloat32(samples)
					if err != nil {
						errCh <- fmt.Errorf("resampling: %w", err)
						cancel()
						return
					}

					if len(output) > 0 {
						select {
						case resampledAudio <- output:
						case <-ctx.Done():
							log.Println("main: Resampling goroutine stopped (context cancelled)")
							return
						default:
							log.Println("main: resampledAudio channel full, dropping samples")
						}
					}

				case <-ctx.Done():
					log.Println("main: Resampling goroutine stopped (context cancelled)")
					return
				}
			}
		}()

		// 3. Transcription Processing Goroutine
		// Feeds audio to Sherpa, decodes, detects endpoints
		go func() {
			defer close(results)
			log.Println("main: Transcription processing goroutine started")

			var lastText string

			for {
				select {
				case samples, ok := <-resampledAudio:
					if !ok {
						log.Println("main: Transcription processing goroutine stopped (channel closed)")
						return
					}

					// Feed samples to recognizer at MODEL_RATE
					stream.AcceptWaveform(MODEL_RATE, samples)

					// Drain any remaining samples in resampledAudio channel
				drainLoop:
					for {
						select {
						case samples, ok := <-resampledAudio:
							if !ok {
								log.Println("main: Channel closed while draining")
								return
							}
							stream.AcceptWaveform(MODEL_RATE, samples)
						default:
							// No more samples available, exit drain loop
							break drainLoop
						}
					}

					// Decode when ready
					for recognizer.IsReady(stream) {
						recognizer.Decode(stream)
					}

					// Get current result
					havePartial := false
					text := recognizer.GetResult(stream).Text

					// Check for new partial result
					if len(text) != 0 && lastText != text {
						lastText = text
						log.Printf("main: partial result: %s\n", lastText)
						havePartial = true
					}

					isEndpoint := recognizer.IsEndpoint(stream)
					if isEndpoint {
						log.Println("main: endpoint detected")
						recognizer.Reset(stream)
						lastText = ""
					}

					// Send updates - skip if same as last and not endpoint
					if havePartial || isEndpoint {
						update := TranscriptionUpdate{
							NewText:    text,
							IsEndpoint: isEndpoint,
						}

						log.Printf("main: transcription update: %+v\n", update)
						select {
						case results <- update:
						case <-ctx.Done():
							log.Println("main: Transcription processing goroutine stopped (context cancelled)")
							return
						}
					}

				case <-ctx.Done():
					log.Println("main: Transcription processing goroutine stopped (context cancelled)")
					return
				}
			}
		}()

		// 4. Text Output Goroutine
		// Receives transcribed text and types via dotool
		go func() {
			log.Println("main: Text output goroutine started")

			// Start persistent dotool process
			dotool := exec.CommandContext(ctx, "dotool")
			stdin, err := dotool.StdinPipe()
			if err != nil {
				errCh <- fmt.Errorf("dotool stdin pipe: %w", err)
				cancel()
				return
			}

			dotool.Stderr = os.Stderr
			if err := dotool.Start(); err != nil {
				errCh <- fmt.Errorf("dotool start: %w", err)
				cancel()
				return
			}

			// Track the currently displayed text
			var displayedText string

			// Cleanup function
			defer func() {
				stdin.Close()
				dotool.Wait()
				cancel()
				log.Println("main: Text output goroutine stopped")
			}()

			for {
				select {
				case textUpdate, ok := <-results:
					if !ok {
						log.Println("main: Text output goroutine stopped (channel closed)")
						return
					}

					config.UI.Chan <- &gui.HideMsg{}
					text := textUpdate.NewText
					// Compute the difference
					update := ComputeTextUpdate(displayedText, text)
					log.Printf("update_text called\n")
					log.Printf("  old_text='%s' (len=%d)\n", displayedText, len(displayedText))
					log.Printf("  new_text='%s' (len=%d)\n", text, len(text))
					log.Printf("  %s\n", update)

					// Send backspace commands
					if update.DeleteCount > 0 {
						backspaces := strings.Repeat(" backspace", update.DeleteCount)
						if _, err := io.WriteString(stdin, fmt.Sprintf("key%s\n", backspaces)); err != nil {
							errCh <- fmt.Errorf("dotool write backspace: %w", err)
							cancel()
							return
						}
					}

					// Send new text to append
					if len(update.ToAppend) > 0 {
						if _, err := io.WriteString(stdin, fmt.Sprintf("type %s\n", update.ToAppend)); err != nil {
							errCh <- fmt.Errorf("dotool write type: %w", err)
							cancel()
							return
						}
					}

					// Update the displayed text state
					if textUpdate.IsEndpoint {
						// On endpoint, reset displayed text
						displayedText = ""
					} else {
						displayedText = text
					}

				case <-ctx.Done():
					log.Println("main: Text output goroutine stopped (context cancelled)")
					return
				}
			}
		}()

		// Wait for stop signal or error
		for {
			select {
			case <-ctx.Done():
				break
			case sig := <-sigChan:
				switch sig {
				case syscall.SIGUSR1:
					log.Println("listen: received record signal, but already recording")
				case syscall.SIGUSR2:
					log.Println("main: received stop signal")
				case syscall.SIGTERM:
					cancel()
					break ForListen
				}
				break
			case err := <-errCh:
				if err != nil && !errors.Is(err, context.Canceled) {
					log.Println("main: error during recording: ", err)
				}
				break
			}
			break
		}

		config.UI.Chan <- &gui.ShowStoppingMsg{}
		log.Println("main: stopping recording session...")

		// Cancel context to stop all goroutines
		cancel()

		// Close audio samples channel to trigger cleanup cascade
		close(audioSamples)

		// Wait a moment for goroutines to finish cleanly
		time.Sleep(100 * time.Millisecond)

		// Cleanup Sherpa stream
		sherpa.DeleteOnlineStream(stream)

		// Set state back to idle
		if err := pid.WriteState(statePath, false); err != nil {
			log.Println("main: failed to write idle state: ", err)
		}

		log.Println("main: recording session finished, ready for next session")
	}

	log.Println("main: listen function exiting")
}
