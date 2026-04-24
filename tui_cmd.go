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

	// Create pipes manually instead of using cmd.StdoutPipe/StderrPipe
	// so that cmd.Wait() doesn't close our read ends. This lets us
	// read stderr after the process exits for early-crash diagnostics.
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		log.Fatalln("tui: failed to create stdout pipe:", err)
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		log.Fatalln("tui: failed to create stderr pipe:", err)
	}
	cmd.Stdout = stdoutWrite
	cmd.Stderr = stderrWrite

	if err := cmd.Start(); err != nil {
		log.Fatalln("tui: failed to start listen subprocess:", err)
	}
	stdoutWrite.Close()
	stderrWrite.Close()

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	// Wait for socket to appear, checking for early subprocess exit.
	var client *ipc.Client
	for i := 0; i < 50; i++ {
		select {
		case waitErr := <-exited:
			remaining, _ := io.ReadAll(stderrRead)
			if len(remaining) > 0 {
				fmt.Fprintf(os.Stderr, "%s", remaining)
			}
			if waitErr != nil {
				log.Fatalln("tui: listen subprocess exited early:", waitErr)
			}
			log.Fatalln("tui: listen subprocess exited before creating socket")
		default:
		}
		time.Sleep(100 * time.Millisecond)
		client, err = ipc.Connect(socketPath)
		if err == nil {
			break
		}
	}
	if client == nil {
		cmd.Process.Kill()
		<-exited
		remaining, _ := io.ReadAll(stderrRead)
		if len(remaining) > 0 {
			fmt.Fprintf(os.Stderr, "%s", remaining)
		}
		log.Fatalln("tui: failed to connect to listen subprocess:", err)
	}

	// Run TUI
	tuiErr := tui.Run(client, stdoutRead, stderrRead)
	client.Close()
	stdoutRead.Close()
	stderrRead.Close()

	// Clean up subprocess
	cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		<-exited
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
