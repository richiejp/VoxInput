package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	openairt "github.com/WqyJh/go-openai-realtime"
	"github.com/gen2brain/malgo"
	"github.com/sashabaranov/go-openai"

	"github.com/richiejp/VoxInput/internal/audio"
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

// TODO: Reimplment replay
func listen(pidPath, apiKey, httpApiBase, wsApiBase, lang, model string, timeout time.Duration) {
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

	clientConfig := openai.DefaultConfig(apiKey)
	clientConfig.BaseURL = httpApiBase
	clientConfig.HTTPClient = &http.Client{
		Timeout: timeout,
	}
	client := openai.NewClientWithConfig(clientConfig)

	rtConf := openairt.DefaultConfig(apiKey)
	rtConf.BaseURL = wsApiBase
	rtConf.APIBaseURL = httpApiBase
	rtConf.HTTPClient = &http.Client{Timeout: timeout}
	rtCli := openairt.NewClientWithConfig(rtConf)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGUSR1)
	signal.Notify(sigChan, syscall.SIGUSR2)
	signal.Notify(sigChan, syscall.SIGTERM)

	err = pid.Write(pidPath)
	defer func() {
		if err := os.Remove(pidPath); err != nil {
			log.Println("main: failed to remove PID file: ", err)
		}
	}()

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

		initCtx, finishInit := context.WithTimeout(ctx, timeout)
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
					Model:    model,
					Language: lang,
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

		// ui.Chan <- &gui.ShowListeningMsg{}

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
					// ui.Chan <- &gui.ShowSpeechDetectedMsg{}
				case openairt.ServerEventTypeInputAudioBufferSpeechStopped:
					// ui.Chan <- &gui.ShowTranscribingMsg{}
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

				// ui.Chan <- &gui.HideMsg{}

				log.Println("main: received transcribed text: ", text)
				req := openai.ChatCompletionRequest{
					Model: "gemma-3-4b-it-qat",
					Messages: []openai.ChatCompletionMessage{
						{
							Role:    openai.ChatMessageRoleSystem,
							Content: "You are a bubbly teacher who answers questions for small children and provides them with curious facts and ideas",
						},
						{
							Role:    openai.ChatMessageRoleUser,
							Content: text,
						},
					},
					MaxTokens: 512,
				}

				log.Println("main: Creating chat completion")
				resp, err := client.CreateChatCompletion(context.Background(), req)
				if err != nil {
					log.Printf("Chat completion error: %v\n", err)
					continue
				}
				
				// Print the response
				if len(resp.Choices) > 0 {
					log.Println("Assistant:", resp.Choices[0].Message.Content)
				} else {
					log.Println("No response received")
				}

				// TODO: Insert TTS here?
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

		// ui.Chan <- &gui.ShowStoppingMsg{}
		log.Println("main: finished transcribing")
		conn.Close()
		cancel()

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
