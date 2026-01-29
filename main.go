package main

import (
	"context"
	_ "embed"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/richiejp/VoxInput/internal/audio"
	"github.com/richiejp/VoxInput/internal/gui"
	"github.com/richiejp/VoxInput/internal/pid"
	"github.com/richiejp/VoxInput/internal/semver"
)

//go:embed version.txt
var version []byte

func main() {

	if err := semver.SetVersion(version); err != nil {
		fmt.Printf("Version format error '%s': %v\n", string(version), err)
		os.Exit(1)
	}

	if len(os.Args) < 2 {
		fmt.Println("Expected 'listen', 'record', 'write', 'toggle', 'status', or 'help' subcommands")
		os.Exit(1)
	}

	cmd := os.Args[1]

	switch cmd {
	case "help":
		fmt.Println(`Available commands:
  listen - Start speech to text daemon
           --replay play the audio just recorded for transcription
           --no-realtime use the HTTP API instead of the realtime API; disables VAD
           --no-show-status don't show when recording has started or stopped
           --output-file <path> Write transcribed text to file instead of keyboard
           --prompt <text> Text used to condition model output. Could be previously transcribed text or uncommon words you expect to use
           --mode <transcription|assistant> (realtime only, default: transcription)
           --instructions <text> System prompt for the assistant model

  record - Tell existing listener to start recording audio. In realtime mode it also begins transcription
  write  - Tell existing listener to stop recording audio and begin transcription if not in realtime mode
  stop   - Alias for write; makes more sense in realtime mode
  toggle - Toggle recording on/off (start recording if idle, stop if recording)
  status - Show whether the server is listening and if it's currently recording
  devices - List capture devices
  help   - Show this help message
  ver    - Print version

Environment variables:
  VOXINPUT_API_KEY or OPENAI_API_KEY - OpenAI API key (default: sk-xxx)
  VOXINPUT_BASE_URL or OPENAI_BASE_URL - HTTP API base URL (default: http://localhost:8080/v1)
  VOXINPUT_WS_BASE_URL or OPENAI_WS_BASE_URL - WebSocket API base URL (default: ws://localhost:8080/v1/realtime)
  VOXINPUT_LANG or LANG - Language code for transcription (default: none)
  VOXINPUT_TRANSCRIPTION_MODEL or TRANSCRIPTION_MODEL - Transcription model (default: whisper-1)
  VOXINPUT_ASSISTANT_MODEL or ASSISTANT_MODEL - Assistant model (default: gpt-realtime)
  VOXINPUT_ASSISTANT_VOICE or ASSISTANT_VOICE - Assistant voice (default: none)
	VOXINPUT_ASSISTANT_INSTRUCTIONS - System prompt for the assistant model (default: none)
  VOXINPUT_TRANSCRIPTION_TIMEOUT or TRANSCRIPTION_TIMEOUT - Transcription timeout (default: 30s)
  VOXINPUT_SHOW_STATUS or SHOW_STATUS - Show status notifications (yes/no, default: yes)
  VOXINPUT_CAPTURE_DEVICE - Name of the capture device (default: system default; use 'devices' to list)
  VOXINPUT_OUTPUT_FILE - File to write transcribed text to (instead of keyboard)
  VOXINPUT_PROMPT - Text used to condition the transcription model output. Could be previously transcribed text or uncommon words you expect to use (default: none)
  VOXINPUT_MODE - Realtime mode (transcription|assistant, default: transcription)
  XDG_RUNTIME_DIR - Directory for PID and state files (required, standard XDG variable)`)
		return
	case "ver":
		fmt.Printf("v%s\n", strings.TrimSpace(string(version)))
		return
	default:
	}

	pidPath, err := pid.Path()
	if err != nil {
		log.Fatalln("main: failed to get PID file path: ", err)
	}

	statePath, err := pid.StatePath()
	if err != nil {
		log.Fatalln("main: failed to get state file path: ", err)
	}

	if cmd == "listen" {
		apiKey := getOpenaiEnv("API_KEY", "sk-xxx")
		httpApiBase := getOpenaiEnv("BASE_URL", "http://localhost:8080/v1")
		wsApiBase := getOpenaiEnv("WS_BASE_URL", "ws://localhost:8080/v1/realtime")
		lang := getPrefixedEnv([]string{"VOXINPUT", ""}, "LANG", "")
		model := getPrefixedEnv([]string{"VOXINPUT", ""}, "TRANSCRIPTION_MODEL", "whisper-1")
		assistantModel := getPrefixedEnv([]string{"VOXINPUT", ""}, "ASSISTANT_MODEL", "gpt-realtime")
		assistantVoice := getPrefixedEnv([]string{"VOXINPUT", ""}, "ASSISTANT_VOICE", "")
		instructions := getPrefixedEnv([]string{"VOXINPUT", ""}, "ASSISTANT_INSTRUCTIONS", "")
		timeoutStr := getPrefixedEnv([]string{"VOXINPUT", ""}, "TRANSCRIPTION_TIMEOUT", "30s")
		showStatusText := getPrefixedEnv([]string{"VOXINPUT", ""}, "SHOW_STATUS", "yes")
		captureDeviceName := getPrefixedEnv([]string{"VOXINPUT"}, "CAPTURE_DEVICE", "")
		prompt := getPrefixedEnv([]string{"VOXINPUT"}, "PROMPT", "")
		outputFile := getPrefixedEnv([]string{"VOXINPUT"}, "OUTPUT_FILE", "")

		mode := getPrefixedEnv([]string{"VOXINPUT"}, "MODE", "transcription")

		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			log.Println("main: failed to parse timeout", err)
			timeout = time.Second * 30
		}

		if len(lang) > 2 {
			lang = lang[:2]
		}

		if lang != "" {
			log.Println("main: language is set to ", lang)
		}

		if slices.Contains(os.Args[2:], "--no-show-status") {
			showStatusText = "no"
		}
		showStatus := !(showStatusText == "no" || showStatusText == "false")

		replay := slices.Contains(os.Args[2:], "--replay")
		realtime := !slices.Contains(os.Args[2:], "--no-realtime")

		var outputFileArg string
		for i := 2; i < len(os.Args); i++ {
			arg := os.Args[i]
			if arg == "--output-file" && i+1 < len(os.Args) {
				outputFileArg = os.Args[i+1]
				break
			}
		}
		if outputFileArg != "" {
			outputFile = outputFileArg
		}

		var promptArg string
		for i := 2; i < len(os.Args); i++ {
			arg := os.Args[i]
			if arg == "--prompt" && i+1 < len(os.Args) {
				promptArg = os.Args[i+1]
				break
			}
		}
		if promptArg != "" {
			prompt = promptArg
		}

		var modeArg string
		for i := 2; i < len(os.Args); i++ {
			arg := os.Args[i]
			if arg == "--mode" && i+1 < len(os.Args) {
				modeArg = os.Args[i+1]
				break
			}
		}
		if modeArg != "" {
			mode = modeArg
		}

		var instructionsArg string
		for i := 2; i < len(os.Args); i++ {
			arg := os.Args[i]
			if arg == "--instructions" && i+1 < len(os.Args) {
				instructionsArg = os.Args[i+1]
				break
			}
		}
		if instructionsArg != "" {
			instructions = instructionsArg
		}

		if realtime {
			ctx, cancel := context.WithCancel(context.Background())
			ui := gui.New(ctx, showStatus)

			go func() {
				listen(ListenConfig{
					PIDPath:        pidPath,
					APIKey:         apiKey,
					HTTPAPIBase:    httpApiBase,
					WSAPIBase:      wsApiBase,
					Lang:           lang,
					Model:          model,
					Timeout:        timeout,
					UI:             ui,
					CaptureDevice:  captureDeviceName,
					OutputFile:     outputFile,
					Prompt:         prompt,
					Mode:           mode,
					AssistantModel: assistantModel,
					AssistantVoice: assistantVoice,
					Instructions:   instructions,
				})
				cancel()
			}()

			ui.Run()
		} else {
			listenOld(pidPath, apiKey, httpApiBase, lang, model, replay, timeout)
		}

		return
	}

	id, err := pid.Read(pidPath)
	if err != nil {
		log.Fatalln("main: failed to read listener PID: ", err)
	}

	proc, err := os.FindProcess(id)
	if err != nil {
		log.Fatalln("main: Failed to find listen process: ", err)
	}

	switch cmd {
	case "record":
		log.Println("main: Sending record signal")
		err = proc.Signal(syscall.SIGUSR1)
	case "stop":
		fallthrough
	case "write":
		log.Println("main: Sending stop/write signal")
		err = proc.Signal(syscall.SIGUSR2)
	case "status":
		err = proc.Signal(syscall.Signal(0))
		if err != nil {
			log.Fatalln("main: Failed to signal listen process: ", err)
		}

		recording, err := pid.ReadState(statePath)
		if err != nil {
			log.Fatalln("main: Failed to read state file: ", err)
		}

		if recording {
			fmt.Println("recording")
		} else {
			fmt.Println("idle")
		}
	case "toggle":
		// Read current state
		recording, readErr := pid.ReadState(statePath)
		if readErr != nil {
			log.Fatalln("main: Failed to read state: ", readErr)
		}

		if recording {
			log.Println("main: Currently recording, sending stop signal")
			err = proc.Signal(syscall.SIGUSR2)
		} else {
			log.Println("main: Currently idle, sending record signal")
			err = proc.Signal(syscall.SIGUSR1)
		}
	case "devices":
		err := audio.ListCaptureDevices()
		if err != nil {
			log.Fatalln("Failed to enumerate devices:", err)
		}

		return
	default:
		log.Fatalln("main: Unknown command: ", os.Args[1])
	}

	if err != nil {
		log.Fatalln("main: Error sending signal: ", err)
	}
}
