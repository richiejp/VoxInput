package localvqe

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func exeDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot determine executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("cannot resolve executable symlinks: %w", err)
	}
	return filepath.Dir(exe), nil
}

func findFirst(candidates []string) string {
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func libName() string {
	if runtime.GOOS == "darwin" {
		return "liblocalvqe.dylib"
	}
	return "liblocalvqe.so"
}

// EnsureLib returns a path to the localvqe shared library.
// If libPath is non-empty, it's returned as-is (user override).
// Otherwise looks for the library relative to the executable.
func EnsureLib(libPath string) (string, error) {
	if libPath != "" {
		return libPath, nil
	}

	name := libName()
	dir, err := exeDir()
	if err != nil {
		return "", err
	}

	if p := findFirst([]string{
		filepath.Join(dir, name),
		filepath.Join(dir, "..", "lib", name),
		filepath.Join(dir, "lib", name),
	}); p != "" {
		return p, nil
	}

	// Fall back to bare name (system library path / dlopen search)
	return name, nil
}

// EnsureModel returns a path to the GGUF model file.
// If modelPath is non-empty, it's returned as-is (user override).
// Otherwise looks for the model relative to the executable.
func EnsureModel(modelPath string) (string, error) {
	if modelPath != "" {
		if _, err := os.Stat(modelPath); err != nil {
			return "", fmt.Errorf("model file not found: %w", err)
		}
		return modelPath, nil
	}

	dir, err := exeDir()
	if err != nil {
		return "", err
	}

	candidates := []string{
		filepath.Join(dir, "share", "voxinput", "localvqe.gguf"),
		filepath.Join(dir, "..", "share", "voxinput", "localvqe.gguf"),
	}
	if p := findFirst(candidates); p != "" {
		return p, nil
	}

	return "", fmt.Errorf("model not found (searched %v; set VOXINPUT_LOCALVQE_MODEL or rebuild with CMake)", candidates)
}
