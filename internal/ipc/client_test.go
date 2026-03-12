package ipc

import (
	"path/filepath"
	"testing"
	"time"
)

func TestClientConnectAndClose(t *testing.T) {
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
	if err := cli.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestClientReadEvent(t *testing.T) {
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
	srv.Broadcast(Event{Kind: EventTranscript, Ts: 123, Text: "test text", IsUser: true})

	e, err := cli.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent: %v", err)
	}
	if e.Kind != EventTranscript || e.Text != "test text" || !e.IsUser {
		t.Errorf("got %+v, want transcript 'test text' isUser=true", e)
	}
}

func TestClientSendCommand(t *testing.T) {
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

	if err := cli.SendCommand(Command{Kind: CommandStop}); err != nil {
		t.Fatalf("SendCommand: %v", err)
	}

	select {
	case cmd := <-srv.Commands():
		if cmd.Kind != CommandStop {
			t.Errorf("got %v, want %v", cmd.Kind, CommandStop)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

func TestClientReconnectFails(t *testing.T) {
	_, err := Connect("/tmp/nonexistent-voxinput-test.sock")
	if err == nil {
		t.Error("expected error connecting to nonexistent socket")
	}
}
