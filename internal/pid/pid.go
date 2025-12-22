package pid

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
)

func Path() (string, error) {
	voxDir := os.Getenv("VOXINPUT_RUNTIME_DIR")
	if voxDir != "" {
		p := filepath.Join(voxDir, "VoxInput.pid")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	p := filepath.Join("/run/voxinput", "VoxInput.pid")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}

	runtimeDir := os.Getenv("VOXINPUT_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = os.Getenv("XDG_RUNTIME_DIR")
		if runtimeDir == "" {
			runtimeDir = "/run/voxinput"
		}
	}

	return filepath.Join(runtimeDir, "VoxInput.pid"), nil
}

func Write(path string) error {
	pid := os.Getpid()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

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

func StatePath() (string, error) {
	voxDir := os.Getenv("VOXINPUT_RUNTIME_DIR")
	if voxDir != "" {
		p := filepath.Join(voxDir, "VoxInput.state")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	p := filepath.Join("/run/voxinput", "VoxInput.state")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}

	runtimeDir := os.Getenv("VOXINPUT_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = os.Getenv("XDG_RUNTIME_DIR")
		if runtimeDir == "" {
			runtimeDir = "/run/voxinput"
		}
	}

	return filepath.Join(runtimeDir, "VoxInput.state"), nil
}

func WriteState(path string, recording bool) error {
	state := "idle"
	if recording {
		state = "recording"
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	err := os.WriteFile(path, []byte(state), 0644)
	if err != nil {
		return fmt.Errorf("Failed to write state file: %w", err)
	}

	return nil
}

func ReadState(path string) (bool, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("pid: failed to read state file: %s: %w", path, err)
	}

	state := string(buf)
	return state == "recording", nil
}
