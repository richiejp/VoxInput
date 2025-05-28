package main

import (
	"fmt"
	"log"
	"os"
	"slices"
	"syscall"

	"github.com/richiejp/VoxInput/internal/pid"
)

// Sync with flake
const version = "0.4.0"

func main() {

	if len(os.Args) < 2 {
		fmt.Println("Expected 'listen', 'record', 'write', or 'help' subcommands")
		os.Exit(1)
	}

	cmd := os.Args[1]

	switch cmd {
	case "help":
		fmt.Println("Available commands:")
		fmt.Println("  listen - Start speech to text daemon")
		fmt.Println("           --replay play the audio just recorded for transcription")
		fmt.Println("           --no-realtime use the HTTP API instead of the realtime API; disables VAD")
		fmt.Println("  record - Tell existing listener to start recording audio. In realtime mode it also begins transcription")
		fmt.Println("  write  - Tell existing listener to stop recording audio and begin transcription if not in realtime mode")
		fmt.Println("  stop   - Alias for write; makes more sense in realtime mode")
		fmt.Println("  help   - Show this help message")
		fmt.Println("  ver    - Print version")
		return
	case "ver":
		fmt.Printf("v%s\n", version)
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

		if len(lang) > 2 {
			lang = lang[:2]
		}

		if lang != "" {
			log.Println("main: language is set to ", lang)
		}

		replay := slices.Contains(os.Args[2:], "--replay")
		realtime := !slices.Contains(os.Args[2:], "--no-realtime")

		if realtime {
			listen(pidPath, apiKey, httpApiBase, wsApiBase, lang, model)
		} else {
			listenOld(pidPath, apiKey, httpApiBase, lang, model, replay)
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
	default:
		log.Fatalln("main: Unknown command: ", os.Args[1])
	}

	if err != nil {
		log.Fatalln("main: Error sending signal: ", err)
	}
}
