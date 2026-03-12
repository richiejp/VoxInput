package ipc

import (
	"path/filepath"
	"testing"
	"time"
)

func TestServerAcceptClient(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "test.sock")
	srv, err := NewServer(sock)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	cli, err := Connect(sock)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer cli.Close()

	// Verify we can communicate
	srv.Broadcast(Event{Kind: EventStatus, Ts: 1, Text: "hello"})
	e, err := cli.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent: %v", err)
	}
	if e.Text != "hello" {
		t.Errorf("got text %q, want %q", e.Text, "hello")
	}
}

func TestServerBroadcast(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "test.sock")
	srv, err := NewServer(sock)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	cli1, err := Connect(sock)
	if err != nil {
		t.Fatalf("Connect cli1: %v", err)
	}
	defer cli1.Close()

	cli2, err := Connect(sock)
	if err != nil {
		t.Fatalf("Connect cli2: %v", err)
	}
	defer cli2.Close()

	// Allow connections to be established
	time.Sleep(50 * time.Millisecond)

	srv.Broadcast(Event{Kind: EventTranscript, Ts: 42, Text: "test"})

	e1, err := cli1.ReadEvent()
	if err != nil {
		t.Fatalf("cli1 ReadEvent: %v", err)
	}
	e2, err := cli2.ReadEvent()
	if err != nil {
		t.Fatalf("cli2 ReadEvent: %v", err)
	}

	if e1.Text != "test" || e2.Text != "test" {
		t.Errorf("broadcast mismatch: cli1=%q cli2=%q", e1.Text, e2.Text)
	}
}

func TestServerReceiveCommand(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "test.sock")
	srv, err := NewServer(sock)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	cli, err := Connect(sock)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer cli.Close()

	time.Sleep(50 * time.Millisecond)

	if err := cli.SendCommand(Command{Kind: CommandRecord}); err != nil {
		t.Fatalf("SendCommand: %v", err)
	}

	select {
	case cmd := <-srv.Commands():
		if cmd.Kind != CommandRecord {
			t.Errorf("got command %v, want %v", cmd.Kind, CommandRecord)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for command")
	}
}

func TestServerClientDisconnect(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "test.sock")
	srv, err := NewServer(sock)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	cli, err := Connect(sock)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	cli.Close()
	time.Sleep(50 * time.Millisecond)

	// Should not panic or block
	srv.Broadcast(Event{Kind: EventStatus, Ts: 1, Text: "after disconnect"})
}

func TestServerSlowClient(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "test.sock")
	srv, err := NewServer(sock)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	cli, err := Connect(sock)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer cli.Close()

	time.Sleep(50 * time.Millisecond)

	// Broadcast many events without reading
	for i := 0; i < 100; i++ {
		srv.Broadcast(Event{Kind: EventLog, Ts: int64(i), Text: "flood"})
	}
	// Should not block or panic
}

func TestServerClose(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "test.sock")
	srv, err := NewServer(sock)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	cli, err := Connect(sock)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	srv.Close()

	// Client should get an error on next read
	_, err = cli.ReadEvent()
	if err == nil {
		t.Error("expected error after server close")
	}
	cli.Close()
}

func TestServerStaleSocketCleanup(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "test.sock")

	// Create first server
	srv1, err := NewServer(sock)
	if err != nil {
		t.Fatalf("NewServer 1: %v", err)
	}
	srv1.Close()

	// Create second server on same path (stale socket)
	srv2, err := NewServer(sock)
	if err != nil {
		t.Fatalf("NewServer 2 (stale socket): %v", err)
	}
	defer srv2.Close()

	cli, err := Connect(sock)
	if err != nil {
		t.Fatalf("Connect to second server: %v", err)
	}
	cli.Close()
}
