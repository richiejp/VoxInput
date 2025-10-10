package main

import (
	// "context"
	_ "embed"
	"fmt"
	"log"
	"os"

	// "slices"
	"strings"
	"syscall"
	"time"

	// "github.com/richiejp/VoxInput/internal/gui"
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
		fmt.Println("Expected 'listen', 'record', 'write', or 'help' subcommands")
		os.Exit(1)
	}

	cmd := os.Args[1]

	switch cmd {
	case "help":
		fmt.Println("Available commands:")
		fmt.Println("  listen - Start speech to text daemon")
		// fmt.Println("           --replay play the audio just recorded for transcription")
		// fmt.Println("           --no-realtime use the HTTP API instead of the realtime API; disables VAD")
		// fmt.Println("           --no-show-status don't show when recording has started or stopped")
		fmt.Println("  record - Tell existing listener to start recording audio. In realtime mode it also begins transcription")
		fmt.Println("  write  - Tell existing listener to stop recording audio and begin transcription if not in realtime mode")
		fmt.Println("  stop   - Alias for write; makes more sense in realtime mode")
		fmt.Println("  help   - Show this help message")
		fmt.Println("  ver    - Print version")
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

	if cmd == "listen" {
		apiKey := getOpenaiEnv("API_KEY", "sk-xxx")
		httpApiBase := getOpenaiEnv("BASE_URL", "http://localhost:8080/v1")
		wsApiBase := getOpenaiEnv("WS_BASE_URL", "ws://localhost:8080/v1/realtime")
		lang := getPrefixedEnv([]string{"VOXINPUT", ""}, "LANG", "")
		model := getPrefixedEnv([]string{"VOXINPUT", ""}, "TRANSCRIPTION_MODEL", "whisper-1")
		chatModel := getPrefixedEnv([]string{"VOXINPUT", ""}, "CHAT_MODEL", "qwen_qwen3-30b-a3b-instruct-2507")
		ttsModel := getPrefixedEnv([]string{"VOXINPUT", ""}, "TTS_MODEL", "chatterbox-lucilla")
		timeoutStr := getPrefixedEnv([]string{"VOXINPUT", ""}, "TRANSCRIPTION_TIMEOUT", "30s")
		// showStatus := getPrefixedEnv([]string{"VOXINPUT", ""}, "SHOW_STATUS", "yes")

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

		// if showStatus == "no" || showStatus == "false" {
		// 	showStatus = ""
		// }
		//
		// if slices.Contains(os.Args[2:], "--no-show-status") {
		// 	showStatus = ""
		// }

		// replay := slices.Contains(os.Args[2:], "--replay")
		// realtime := !slices.Contains(os.Args[2:], "--no-realtime")

		// if realtime {
		// ctx, cancel := context.WithCancel(context.Background())
		// ui := gui.New(ctx, showStatus)

		// go func() {
		listen(pidPath, apiKey, httpApiBase, wsApiBase, lang, model, chatModel, ttsModel, timeout)
		// cancel()
		// }()

		// ui.Run()
		// } else {
		// listenOld(pidPath, apiKey, httpApiBase, lang, model, replay, timeout)
		// }

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
	default:
		log.Fatalln("main: Unknown command: ", os.Args[1])
	}

	if err != nil {
		log.Fatalln("main: Error sending signal: ", err)
	}
}
