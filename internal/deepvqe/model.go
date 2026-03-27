package deepvqe

import (
	"fmt"
	"os"
	"path/filepath"
)

// EnsureModel returns a path to the GGUF model file.
// If modelPath is non-empty, it's returned as-is (user override).
// Otherwise looks for the model relative to the executable at ../share/voxinput/deepvqe.gguf.
func EnsureModel(modelPath string) (string, error) {
	if modelPath != "" {
		if _, err := os.Stat(modelPath); err != nil {
			return "", fmt.Errorf("model file not found: %w", err)
		}
		return modelPath, nil
	}

	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot determine executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("cannot resolve executable symlinks: %w", err)
	}

	modelPath = filepath.Join(filepath.Dir(exe), "..", "share", "voxinput", "deepvqe.gguf")
	if _, err := os.Stat(modelPath); err != nil {
		return "", fmt.Errorf("model not found at %s: %w (set VOXINPUT_DEEPVQE_MODEL or rebuild with CMake)", modelPath, err)
	}

	return modelPath, nil
}
