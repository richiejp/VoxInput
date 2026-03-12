package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/richiejp/VoxInput/internal/ipc"
)

func newTestModel() Model {
	return Model{
		width:  80,
		height: 24,
	}
}

func TestModelInit(t *testing.T) {
	m := newTestModel()
	if m.activeTab != TabChat {
		t.Errorf("expected initial tab to be Chat, got %v", m.activeTab)
	}
	if len(m.chatLog) != 0 {
		t.Errorf("expected empty chatLog, got %d entries", len(m.chatLog))
	}
	if len(m.logEntries) != 0 {
		t.Errorf("expected empty logEntries, got %d entries", len(m.logEntries))
	}
}

func TestModelTabSwitch(t *testing.T) {
	m := newTestModel()
	if m.activeTab != TabChat {
		t.Fatal("should start on chat tab")
	}

	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	model := m2.(Model)
	if model.activeTab != TabLog {
		t.Error("expected log tab after pressing Tab")
	}

	m3, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = m3.(Model)
	if model.activeTab != TabChat {
		t.Error("expected chat tab after pressing Tab again")
	}
}

func TestModelReceiveStatusEvent(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(ipcEventMsg(ipc.Event{
		Kind: ipc.EventStatus,
		Ts:   1000,
		Text: "Listening...",
	}))
	model := m2.(Model)
	if len(model.chatLog) != 1 {
		t.Fatalf("expected 1 chatLog entry, got %d", len(model.chatLog))
	}
}

func TestModelReceiveTranscriptEvent(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(ipcEventMsg(ipc.Event{
		Kind:   ipc.EventTranscript,
		Ts:     2000,
		Text:   "Hello world",
		IsUser: true,
	}))
	model := m2.(Model)
	if len(model.chatLog) != 1 {
		t.Fatalf("expected 1 chatLog entry, got %d", len(model.chatLog))
	}
	if !strings.Contains(model.chatLog[0], "You:") {
		t.Errorf("expected 'You:' in chat entry, got %q", model.chatLog[0])
	}
}

func TestModelReceiveLogEvent(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(ipcEventMsg(ipc.Event{
		Kind: ipc.EventLog,
		Ts:   3000,
		Text: "debug info",
	}))
	model := m2.(Model)
	if len(model.logEntries) != 1 {
		t.Fatalf("expected 1 logEntry, got %d", len(model.logEntries))
	}
	if len(model.chatLog) != 0 {
		t.Errorf("log events should not appear in chatLog, got %d", len(model.chatLog))
	}
}

func TestModelQuitKey(t *testing.T) {
	m := newTestModel()
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	model := m2.(Model)
	if !model.quitting {
		t.Error("expected quitting=true after 'q'")
	}
	if cmd == nil {
		t.Error("expected quit command")
	}
}

func TestModelWindowResize(t *testing.T) {
	m := newTestModel()
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := m2.(Model)
	if model.width != 120 || model.height != 40 {
		t.Errorf("expected 120x40, got %dx%d", model.width, model.height)
	}
}

func TestModelView(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	view := m.View()
	if !strings.Contains(view, "Chat") {
		t.Error("expected 'Chat' in view")
	}
	if !strings.Contains(view, "Log") {
		t.Error("expected 'Log' in view")
	}
	if !strings.Contains(view, "idle") {
		t.Error("expected 'idle' in status bar")
	}
}
