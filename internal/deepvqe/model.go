package deepvqe

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

const (
	defaultModelURL = "https://huggingface.co/richiejp/deepvqe-aec-gguf/resolve/main/deepvqe.gguf"
	// TODO: set actual checksum after model upload
	modelSHA256 = "3a0869615ac0e888f1198d381f6910c762363e493389c0e50f7583af843aa893"
)

// EnsureModel returns a path to the GGUF model file, downloading it if needed.
// If modelPath is non-empty, it's returned as-is (user override).
// Otherwise checks $XDG_DATA_HOME/voxinput/deepvqe.gguf, downloading if absent.
func EnsureModel(modelPath string) (string, error) {
	if modelPath != "" {
		if _, err := os.Stat(modelPath); err != nil {
			return "", fmt.Errorf("model file not found: %w", err)
		}
		return modelPath, nil
	}

	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
		dataHome = filepath.Join(home, ".local", "share")
	}

	modelDir := filepath.Join(dataHome, "voxinput")
	modelPath = filepath.Join(modelDir, "deepvqe.gguf")

	if _, err := os.Stat(modelPath); err == nil {
		return modelPath, nil
	}

	log.Printf("deepvqe: downloading model to %s", modelPath)
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		return "", fmt.Errorf("create model directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(modelDir, "deepvqe-*.gguf.tmp")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		os.Remove(tmpPath)
	}()

	resp, err := http.Get(defaultModelURL)
	if err != nil {
		return "", fmt.Errorf("download model: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download model: HTTP %d", resp.StatusCode)
	}

	hasher := sha256.New()
	writer := io.MultiWriter(tmpFile, hasher)
	if _, err := io.Copy(writer, resp.Body); err != nil {
		return "", fmt.Errorf("download model: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("close temp file: %w", err)
	}

	if modelSHA256 != "" {
		got := hex.EncodeToString(hasher.Sum(nil))
		if got != modelSHA256 {
			return "", fmt.Errorf("model checksum mismatch: got %s, want %s", got, modelSHA256)
		}
	}

	if err := os.Rename(tmpPath, modelPath); err != nil {
		return "", fmt.Errorf("install model: %w", err)
	}

	log.Printf("deepvqe: model downloaded to %s", modelPath)
	return modelPath, nil
}
