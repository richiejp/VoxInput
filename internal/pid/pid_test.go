package pid

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAndReadPID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pid")

	if err := Write(path); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if got != os.Getpid() {
		t.Errorf("got PID %d, want %d", got, os.Getpid())
	}
}

func TestWriteAndReadState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.state")

	if err := WriteState(path, true); err != nil {
		t.Fatalf("WriteState(true): %v", err)
	}

	recording, err := ReadState(path)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if !recording {
		t.Error("expected recording=true")
	}

	if err := WriteState(path, false); err != nil {
		t.Fatalf("WriteState(false): %v", err)
	}

	recording, err = ReadState(path)
	if err != nil {
		t.Fatalf("ReadState: %v", err)
	}
	if recording {
		t.Error("expected recording=false")
	}
}

func TestReadStateMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.state")
	recording, err := ReadState(path)
	if err != nil {
		t.Fatalf("ReadState nonexistent: %v", err)
	}
	if recording {
		t.Error("expected recording=false for missing file")
	}
}

func TestStatePath(t *testing.T) {
	t.Setenv("VOXINPUT_RUNTIME_DIR", t.TempDir())

	path, err := StatePath()
	if err != nil {
		t.Fatalf("StatePath: %v", err)
	}

	if filepath.Base(path) != "VoxInput.state" {
		t.Errorf("unexpected state filename: %s", path)
	}
}
