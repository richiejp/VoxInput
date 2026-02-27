//go:build darwin

package pid

import (
	"os"
	"path/filepath"
)

func platformDefaultRuntimeDir() string {
	if dir := os.Getenv("TMPDIR"); dir != "" {
		return filepath.Join(dir, "voxinput")
	}
	return "/tmp/voxinput"
}
