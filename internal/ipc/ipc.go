package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"

	"github.com/richiejp/VoxInput/internal/gui"
)

type EventKind string

const (
	EventStatus       EventKind = "status"
	EventTranscript   EventKind = "transcript"
	EventAssistant    EventKind = "assistant"
	EventFunctionCall EventKind = "function_call"
	EventLog          EventKind = "log"
	EventError        EventKind = "error"
)

type Event struct {
	Kind      EventKind `json:"kind"`
	Ts        int64     `json:"ts"`
	Text      string    `json:"text"`
	Detail    string    `json:"detail,omitempty"`
	IsUser    bool      `json:"is_user,omitempty"`
	Recording bool      `json:"recording,omitempty"`
}

type CommandKind string

const (
	CommandRecord CommandKind = "record"
	CommandStop   CommandKind = "stop"
	CommandQuit   CommandKind = "quit"
)

type Command struct {
	Kind CommandKind `json:"kind"`
}

func EncodeEvent(w io.Writer, e Event) error {
	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

func DecodeEvent(scanner *bufio.Scanner) (Event, error) {
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return Event{}, fmt.Errorf("scan event: %w", err)
		}
		return Event{}, io.EOF
	}
	line := scanner.Bytes()
	if len(line) == 0 {
		return Event{}, fmt.Errorf("empty line")
	}
	var e Event
	if err := json.Unmarshal(line, &e); err != nil {
		return Event{}, fmt.Errorf("unmarshal event: %w", err)
	}
	return e, nil
}

func EncodeCommand(w io.Writer, c Command) error {
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal command: %w", err)
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

func DecodeCommand(scanner *bufio.Scanner) (Command, error) {
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return Command{}, fmt.Errorf("scan command: %w", err)
		}
		return Command{}, io.EOF
	}
	line := scanner.Bytes()
	if len(line) == 0 {
		return Command{}, fmt.Errorf("empty line")
	}
	var c Command
	if err := json.Unmarshal(line, &c); err != nil {
		return Command{}, fmt.Errorf("unmarshal command: %w", err)
	}
	return c, nil
}

func EventFromGUIMsg(msg gui.Msg) Event {
	switch m := msg.(type) {
	case *gui.ShowListeningMsg:
		return Event{Kind: EventStatus, Text: "Listening with voice audio detection...", Recording: true}
	case *gui.ShowSpeechDetectedMsg:
		return Event{Kind: EventStatus, Text: "Detected speech..."}
	case *gui.ShowTranscribingMsg:
		return Event{Kind: EventStatus, Text: "Transcribing..."}
	case *gui.ShowSpeechSubmittedMsg:
		return Event{Kind: EventStatus, Text: "Speech submitted..."}
	case *gui.ShowGeneratingResponseMsg:
		return Event{Kind: EventStatus, Text: "Generating response..."}
	case *gui.ShowFunctionCallMsg:
		text := "Calling " + m.FunctionName
		return Event{Kind: EventFunctionCall, Text: text, Detail: m.Arguments}
	case *gui.ShowStoppingMsg:
		return Event{Kind: EventStatus, Text: "Stopping listening"}
	case *gui.ShowTranscriptMsg:
		return Event{Kind: EventTranscript, Text: m.Text, IsUser: m.IsUser}
	case *gui.HideMsg:
		return Event{Kind: EventStatus, Text: ""}
	default:
		return Event{Kind: EventStatus, Text: "unknown"}
	}
}
