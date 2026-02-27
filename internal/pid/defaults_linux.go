//go:build linux

package pid

func platformDefaultRuntimeDir() string {
	return "/run/voxinput"
}
