package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/richiejp/VoxInput/internal/ipc"
	"github.com/richiejp/VoxInput/internal/tui"
)

func tuiCommand(args []string) {
	var connectPath string
	var listenArgs []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--connect":
			if i+1 < len(args) {
				connectPath = args[i+1]
				i++
			} else {
				log.Fatalln("tui: --connect requires a socket path argument")
			}
		default:
			listenArgs = append(listenArgs, args[i])
		}
	}

	if connectPath != "" {
		connectAndRun(connectPath)
		return
	}

	socketPath := ipc.SocketPath()

	// Launch listen as subprocess
	cmdArgs := []string{"listen", "--socket", socketPath}
	cmdArgs = append(cmdArgs, listenArgs...)

	executable, err := os.Executable()
	if err != nil {
		log.Fatalln("tui: failed to find own executable:", err)
	}
	cmd := exec.Command(executable, cmdArgs...)
	cmd.Env = os.Environ()

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		log.Fatalln("tui: failed to create stdout pipe:", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		log.Fatalln("tui: failed to create stderr pipe:", err)
	}

	if err := cmd.Start(); err != nil {
		log.Fatalln("tui: failed to start listen subprocess:", err)
	}

	// Wait for socket to appear
	var client *ipc.Client
	for i := 0; i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
		client, err = ipc.Connect(socketPath)
		if err == nil {
			break
		}
	}
	if client == nil {
		remaining, _ := io.ReadAll(stderrPipe)
		if len(remaining) > 0 {
			fmt.Fprintf(os.Stderr, "listen subprocess stderr:\n%s", remaining)
		}
		log.Fatalln("tui: failed to connect to listen subprocess:", err)
	}

	// Run TUI
	tuiErr := tui.Run(client, stdoutPipe, stderrPipe)
	client.Close()

	// Clean up subprocess
	cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		<-done
	}

	if tuiErr != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", tuiErr)
		os.Exit(1)
	}
}

func connectAndRun(path string) {
	client, err := ipc.Connect(path)
	if err != nil {
		log.Fatalln("tui: failed to connect to", path, ":", err)
	}
	defer client.Close()

	// Handle interrupt gracefully by closing the client,
	// which causes tui.Run to receive a disconnect and exit.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		client.SendCommand(ipc.Command{Kind: ipc.CommandQuit})
		client.Close()
	}()

	if err := tui.Run(client); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		os.Exit(1)
	}
}
