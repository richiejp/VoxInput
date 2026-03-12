package ipc

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/richiejp/VoxInput/internal/gui"
)

func TestEventEncodeDecode(t *testing.T) {
	events := []Event{
		{Kind: EventStatus, Ts: 1000, Text: "Listening...", Recording: true},
		{Kind: EventTranscript, Ts: 2000, Text: "Hello world", IsUser: true},
		{Kind: EventFunctionCall, Ts: 3000, Text: "Calling foo", Detail: `{"arg":"val"}`},
		{Kind: EventLog, Ts: 4000, Text: "some log line"},
		{Kind: EventError, Ts: 5000, Text: "something broke"},
	}

	for _, e := range events {
		var buf bytes.Buffer
		if err := EncodeEvent(&buf, e); err != nil {
			t.Fatalf("encode event %v: %v", e.Kind, err)
		}

		scanner := bufio.NewScanner(&buf)
		got, err := DecodeEvent(scanner)
		if err != nil {
			t.Fatalf("decode event %v: %v", e.Kind, err)
		}

		if got.Kind != e.Kind || got.Ts != e.Ts || got.Text != e.Text || got.Detail != e.Detail || got.IsUser != e.IsUser || got.Recording != e.Recording {
			t.Errorf("round-trip mismatch: got %+v, want %+v", got, e)
		}
	}
}

func TestCommandEncodeDecode(t *testing.T) {
	commands := []Command{
		{Kind: CommandRecord},
		{Kind: CommandStop},
		{Kind: CommandQuit},
	}

	for _, c := range commands {
		var buf bytes.Buffer
		if err := EncodeCommand(&buf, c); err != nil {
			t.Fatalf("encode command %v: %v", c.Kind, err)
		}

		scanner := bufio.NewScanner(&buf)
		got, err := DecodeCommand(scanner)
		if err != nil {
			t.Fatalf("decode command %v: %v", c.Kind, err)
		}

		if got.Kind != c.Kind {
			t.Errorf("round-trip mismatch: got %v, want %v", got.Kind, c.Kind)
		}
	}
}

func TestEventFromGUIMsg(t *testing.T) {
	tests := []struct {
		msg      gui.Msg
		wantKind EventKind
		wantText string
	}{
		{&gui.ShowListeningMsg{}, EventStatus, "Listening with voice audio detection..."},
		{&gui.ShowSpeechDetectedMsg{}, EventStatus, "Detected speech..."},
		{&gui.ShowTranscribingMsg{}, EventStatus, "Transcribing..."},
		{&gui.ShowSpeechSubmittedMsg{}, EventStatus, "Speech submitted..."},
		{&gui.ShowGeneratingResponseMsg{}, EventStatus, "Generating response..."},
		{&gui.ShowStoppingMsg{}, EventStatus, "Stopping listening"},
		{&gui.ShowTranscriptMsg{Text: "hi", IsUser: true}, EventTranscript, "hi"},
		{&gui.ShowFunctionCallMsg{FunctionName: "foo", Arguments: "bar"}, EventFunctionCall, "Calling foo"},
		{&gui.HideMsg{}, EventStatus, ""},
	}

	for _, tt := range tests {
		e := EventFromGUIMsg(tt.msg)
		if e.Kind != tt.wantKind {
			t.Errorf("EventFromGUIMsg(%T): kind = %v, want %v", tt.msg, e.Kind, tt.wantKind)
		}
		if e.Text != tt.wantText {
			t.Errorf("EventFromGUIMsg(%T): text = %q, want %q", tt.msg, e.Text, tt.wantText)
		}
	}

	e := EventFromGUIMsg(&gui.ShowListeningMsg{})
	if !e.Recording {
		t.Error("EventFromGUIMsg(ShowListeningMsg): expected Recording=true")
	}
	e = EventFromGUIMsg(&gui.ShowStoppingMsg{})
	if e.Recording {
		t.Error("EventFromGUIMsg(ShowStoppingMsg): expected Recording=false")
	}
}

func TestDecodeInvalidJSON(t *testing.T) {
	buf := bytes.NewBufferString("not json\n")
	scanner := bufio.NewScanner(buf)
	_, err := DecodeEvent(scanner)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestDecodeEmptyLine(t *testing.T) {
	buf := bytes.NewBufferString("\n")
	scanner := bufio.NewScanner(buf)
	_, err := DecodeEvent(scanner)
	if err == nil {
		t.Error("expected error for empty line")
	}
}
