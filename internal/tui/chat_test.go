package tui

import (
	"strings"
	"testing"

	"github.com/richiejp/VoxInput/internal/ipc"
)

func TestRenderTranscriptEvent(t *testing.T) {
	e := ipc.Event{Kind: ipc.EventTranscript, Ts: 1000, Text: "hello", IsUser: true}
	rendered := renderChatEvent(e)
	if !strings.Contains(rendered, "You:") {
		t.Errorf("expected 'You:' in %q", rendered)
	}
	if !strings.Contains(rendered, "hello") {
		t.Errorf("expected 'hello' in %q", rendered)
	}
}

func TestRenderStatusEvent(t *testing.T) {
	e := ipc.Event{Kind: ipc.EventStatus, Ts: 1000, Text: "Listening..."}
	rendered := renderChatEvent(e)
	if !strings.Contains(rendered, "Listening...") {
		t.Errorf("expected 'Listening...' in %q", rendered)
	}
}

func TestRenderFunctionCallEvent(t *testing.T) {
	e := ipc.Event{Kind: ipc.EventFunctionCall, Ts: 1000, Text: "Calling foo", Detail: `{"bar":"baz"}`}
	rendered := renderChatEvent(e)
	if !strings.Contains(rendered, "Calling foo") {
		t.Errorf("expected 'Calling foo' in %q", rendered)
	}
	if !strings.Contains(rendered, `{"bar":"baz"}`) {
		t.Errorf("expected detail in %q", rendered)
	}
}

func TestRenderEmptyStatus(t *testing.T) {
	e := ipc.Event{Kind: ipc.EventStatus, Ts: 1000, Text: ""}
	rendered := renderChatEvent(e)
	if rendered != "" {
		t.Errorf("expected empty string for empty status, got %q", rendered)
	}
}
