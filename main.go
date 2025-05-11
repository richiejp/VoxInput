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
const version = "0.1.0"

func main() {

	if len(os.Args) < 2 {
		fmt.Println("Expected 'listen', 'record', 'write', or 'help' subcommands")
		os.Exit(1)
	}

	cmd := os.Args[1]

	switch cmd {
	case "help":
		fmt.Println("Available commands:")
		fmt.Println("  listen - Start speech to text daemon (use --replay to play the audio just recorded for transcription)")
		fmt.Println("  record - Tell existing listener to start recording audio")
		fmt.Println("  write  - Tell existing listener to stop recording audio and transcribe it")
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
		replay := slices.Contains(os.Args[2:], "--replay")
		listen(pidPath, replay)
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
