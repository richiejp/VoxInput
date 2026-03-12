package tui

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/richiejp/VoxInput/internal/ipc"
)

const maxScrollback = 1000

type Tab int

const (
	TabChat Tab = iota
	TabLog
)

type ipcEventMsg ipc.Event

type ipcDisconnectedMsg struct{ error }

type subprocessLineMsg string

type Model struct {
	client     *ipc.Client
	activeTab  Tab
	chatLog    []string
	logEntries []string
	chatView   viewport.Model
	logView    viewport.Model
	width      int
	height     int
	recording  bool
	quitting   bool
}

func NewModel(client *ipc.Client) Model {
	chatView := viewport.New(80, 20)
	logView := viewport.New(80, 20)
	return Model{
		client:   client,
		chatView: chatView,
		logView:  logView,
		width:    80,
		height:   24,
	}
}

func (m Model) Init() tea.Cmd {
	return readEvent(m.client)
}

func readEvent(client *ipc.Client) tea.Cmd {
	return func() tea.Msg {
		e, err := client.ReadEvent()
		if err != nil {
			return ipcDisconnectedMsg{err}
		}
		return ipcEventMsg(e)
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			if m.client != nil {
				m.client.SendCommand(ipc.Command{Kind: ipc.CommandQuit})
			}
			return m, tea.Quit
		case "r":
			if m.client != nil {
				m.client.SendCommand(ipc.Command{Kind: ipc.CommandRecord})
				m.recording = true
			}
		case "s":
			if m.client != nil {
				m.client.SendCommand(ipc.Command{Kind: ipc.CommandStop})
				m.recording = false
			}
		case "tab":
			if m.activeTab == TabChat {
				m.activeTab = TabLog
			} else {
				m.activeTab = TabChat
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		contentHeight := m.height - 3 // tab bar + status bar + border
		if contentHeight < 1 {
			contentHeight = 1
		}
		m.chatView.Width = m.width
		m.chatView.Height = contentHeight
		m.logView.Width = m.width
		m.logView.Height = contentHeight
		m.updateViewports()

	case ipcEventMsg:
		e := ipc.Event(msg)
		switch e.Kind {
		case ipc.EventLog:
			line := renderLogEvent(e)
			m.logEntries = appendBounded(m.logEntries, line)
			m.logView.SetContent(strings.Join(m.logEntries, "\n"))
			m.logView.GotoBottom()
		case ipc.EventStatus:
			m.recording = e.Recording
			rendered := renderChatEvent(e)
			if rendered != "" {
				m.chatLog = appendBounded(m.chatLog, rendered)
				m.chatView.SetContent(strings.Join(m.chatLog, "\n"))
				m.chatView.GotoBottom()
			}
		default:
			rendered := renderChatEvent(e)
			if rendered != "" {
				m.chatLog = appendBounded(m.chatLog, rendered)
				m.chatView.SetContent(strings.Join(m.chatLog, "\n"))
				m.chatView.GotoBottom()
			}
		}
		return m, readEvent(m.client)

	case subprocessLineMsg:
		line := renderStderrLine(string(msg))
		m.logEntries = appendBounded(m.logEntries, line)
		m.logView.SetContent(strings.Join(m.logEntries, "\n"))
		m.logView.GotoBottom()

	case ipcDisconnectedMsg:
		m.chatLog = appendBounded(m.chatLog,
			errorStyle.Render("Disconnected: "+msg.Error()))
		m.updateViewports()
		return m, nil
	}

	return m, nil
}

func (m *Model) updateViewports() {
	m.chatView.SetContent(strings.Join(m.chatLog, "\n"))
	m.chatView.GotoBottom()
	m.logView.SetContent(strings.Join(m.logEntries, "\n"))
	m.logView.GotoBottom()
}

func appendBounded(s []string, item string) []string {
	s = append(s, item)
	if len(s) > maxScrollback {
		trimmed := make([]string, maxScrollback)
		copy(trimmed, s[len(s)-maxScrollback:])
		s = trimmed
	}
	return s
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Tab bar
	chatTab := tabInactiveStyle.Render("Chat")
	logTab := tabInactiveStyle.Render("Log")
	if m.activeTab == TabChat {
		chatTab = tabActiveStyle.Render("Chat")
	} else {
		logTab = tabActiveStyle.Render("Log")
	}
	b.WriteString(chatTab + " " + logTab + "\n")

	// Content
	if m.activeTab == TabChat {
		b.WriteString(m.chatView.View())
	} else {
		b.WriteString(m.logView.View())
	}
	b.WriteString("\n")

	// Status bar
	state := "idle"
	if m.recording {
		state = "recording"
	}
	hints := "r:record  s:stop  Tab:switch  q:quit"
	status := fmt.Sprintf(" [%s]  %s", state, hints)
	b.WriteString(statusBarStyle.Render(status))

	return b.String()
}

func Run(client *ipc.Client, subprocessOutputs ...io.Reader) error {
	m := NewModel(client)
	p := tea.NewProgram(m, tea.WithAltScreen())

	for _, r := range subprocessOutputs {
		if r != nil {
			go pipeLines(p, r)
		}
	}

	_, err := p.Run()
	return err
}

func pipeLines(p *tea.Program, r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		p.Send(subprocessLineMsg(scanner.Text()))
	}
}
