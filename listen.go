package main

import (
	"bytes"
	"context"
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

	"github.com/gen2brain/malgo"
	"github.com/sashabaranov/go-openai"

	"github.com/richiejp/VoxInput/internal/audio"
	"github.com/richiejp/VoxInput/internal/pid"
)

func listen(pidPath string, replay bool) {
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
		SampleRate:   16000,
		MalgoContext: mctx.Context,
	}

	clientConfig := openai.DefaultConfig(func() string {
		if key := os.Getenv("VOXINPUT_API_KEY"); key != "" {
			return key
		}
		if key := os.Getenv("OPENAI_API_KEY"); key != "" {
			return key
		}
		return "sk-xxx"
	}())
	clientConfig.BaseURL = func() string {
		if url := os.Getenv("VOXINPUT_BASE_URL"); url != "" {
			return url
		}
		if url := os.Getenv("OPENAI_BASE_URL"); url != "" {
			return url
		}
		return "http://localhost:8080/v1"
	}()
	clientConfig.HTTPClient = &http.Client{
		Timeout: time.Second * 30,
	}

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
		for sig := range sigChan {
			switch sig {
			case syscall.SIGUSR1:
			case syscall.SIGUSR2:
				log.Println("main: Received stop/write signal, but wasn't recording")
				continue
			case syscall.SIGTERM:
				break Listen
			}
			break
		}

		log.Println("main: Recording...")

		var buf bytes.Buffer
		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)

		go func() {
			if err := audio.Capture(ctx, &buf, streamConfig); err != nil {
				errCh <- fmt.Errorf("audio capture: %w", err)
			}
		}()

		for sig := range sigChan {
			switch sig {
			case syscall.SIGUSR1:
				log.Println("main: received record signal, but already recording")
				continue
			case syscall.SIGUSR2:
			case syscall.SIGTERM:
				cancel()
				break Listen
			}
			break
		}
		cancel()

		err = <-errCh
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Fatalln("main: ", err)
		}

		reader := bytes.NewReader(buf.Bytes())

		if replay {
			log.Println("main: Playing...")

			if err := audio.Playback(context.Background(), reader, streamConfig); err != nil && !errors.Is(err, io.EOF) {
				log.Fatalln("main: ", fmt.Errorf("audio playback: %w", err))
			}

			log.Println("main: Playback Done")
		}

		wavHeader := audio.NewWAVHeader(buf.Bytes())
		var headerBuf bytes.Buffer
		if err := wavHeader.Write(&headerBuf); err != nil {
			log.Fatalln("main: ", fmt.Errorf("write wav header: %w", err))
		}

		reader.Seek(0, io.SeekStart)
		wavReader := io.MultiReader(&headerBuf, reader)

		client := openai.NewClientWithConfig(clientConfig)
		req := openai.AudioRequest{
			Model:    "whisper-1",
			FilePath: "S16",
			Reader:   wavReader,
			Language: "en",
		}

		resp, err := client.CreateTranscription(context.Background(), req)
		if err != nil {
			log.Fatalln("main: ", fmt.Errorf("CreateTranscription: %w", err))
		}

		log.Println("main: transcribed text: ", resp.Text)

		dotool := exec.Command("dotool")
		stdin, err := dotool.StdinPipe()
		if err != nil {
			log.Fatalln("main: ", fmt.Errorf("dotool stdin pipe: %w", err))
		}
		dotool.Stderr = os.Stderr
		if err := dotool.Start(); err != nil {
			log.Fatalln("main: ", fmt.Errorf("dotool stderr pipe: %w", err))
		}

		_, err = io.WriteString(stdin, fmt.Sprintf("type %s", resp.Text))
		if err != nil {
			log.Fatalln("main: ", fmt.Errorf("dotool stdin WriteString: %w", err))
		}

		if err := stdin.Close(); err != nil {
			log.Fatalln("main: close dotool stdin: ", err)
		}

		if err := dotool.Wait(); err != nil {
			log.Fatalln("main: dotool wait: ", err)
		}
	}
}
