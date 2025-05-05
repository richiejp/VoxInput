package pid

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
)

func Path() (string, error) {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		return "", fmt.Errorf("XDG_RUNTIME_DIR is not set. Cannot determine a sensible location for the PID file.")
	}

	return filepath.Join(runtimeDir, "VoxInput.pid"), nil
}

func Write(path string) error {
	pid := os.Getpid()

	err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0644)
	if err != nil {
		return fmt.Errorf("Failed to create PID file: %w", err)
	}

	log.Printf("pid: file created at %s with PID %d\n", path, pid)

	return nil
}

func Read(path string) (int, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("pid: failed to read file: %s: %w", path, err)
	}

	pid, err := strconv.Atoi(string(buf))
	if err != nil {
		return 0, fmt.Errorf("pid: failed to parse pid: %s: %w", path, err)
	}

	return pid, nil
}
