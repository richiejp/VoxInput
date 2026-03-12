package gui

import (
	"context"
	"log"

	"github.com/gen2brain/beeep"
)

type Msg interface {
	IsMsg() bool
}

type ShowListeningMsg struct{}
type ShowSpeechDetectedMsg struct{}
type ShowTranscribingMsg struct{}
type ShowSpeechSubmittedMsg struct{}
type ShowGeneratingResponseMsg struct{}
type ShowFunctionCallMsg struct {
	FunctionName string
	Arguments    string
}
type HideMsg struct{}
type ShowStoppingMsg struct{}

type ShowTranscriptMsg struct {
	Text   string
	IsUser bool
}

func (m *ShowListeningMsg) IsMsg() bool          { return true }
func (m *ShowSpeechDetectedMsg) IsMsg() bool     { return true }
func (m *ShowTranscribingMsg) IsMsg() bool       { return true }
func (m *ShowSpeechSubmittedMsg) IsMsg() bool    { return true }
func (m *ShowGeneratingResponseMsg) IsMsg() bool { return true }
func (m *ShowFunctionCallMsg) IsMsg() bool       { return true }
func (m *HideMsg) IsMsg() bool                   { return true }
func (m *ShowStoppingMsg) IsMsg() bool           { return true }
func (m *ShowTranscriptMsg) IsMsg() bool         { return true }

type StatusSink interface {
	Send(msg Msg)
}

type MultiSink struct {
	Sinks []StatusSink
}

func (ms *MultiSink) Send(msg Msg) {
	for _, s := range ms.Sinks {
		s.Send(msg)
	}
}

type GUI struct {
	ctx        context.Context
	Chan       chan Msg
	showStatus bool
}

func New(ctx context.Context, showStatus bool) *GUI {
	g := &GUI{
		ctx:        ctx,
		Chan:       make(chan Msg, 10),
		showStatus: showStatus,
	}

	return g
}

func (g *GUI) Send(msg Msg) {
	g.Chan <- msg
}

func (g *GUI) Run() {
	for {
		select {
		case msg := <-g.Chan:
			if !g.showStatus {
				continue
			}

			title := "VoxInput"
			var text string
			image := iconPath("audio-input-microphone")

			switch msg.(type) {
			case *ShowListeningMsg:
				text = "Listening with voice audio detection..."
				image = iconPath("audio-input-microphone")
			case *ShowSpeechDetectedMsg:
				text = "Detected speech..."
				image = iconPath("audio-input-microphone")
			case *ShowTranscribingMsg:
				text = "Transcribing..."
				image = iconPath("text-x-generic")
			case *ShowSpeechSubmittedMsg:
				text = "Speech submitted..."
				image = iconPath("network-transmit-receive")
			case *ShowGeneratingResponseMsg:
				text = "Generating response..."
				image = iconPath("audio-speakers")
			case *ShowFunctionCallMsg:
				funcMsg := msg.(*ShowFunctionCallMsg)
				text = "Calling " + funcMsg.FunctionName
				if funcMsg.Arguments != "" {
					text += " with: " + funcMsg.Arguments
				}
				image = iconPath("applications-system")
			case *HideMsg:
				continue
			case *ShowStoppingMsg:
				text = "Stopping listening"
				image = iconPath("media-playback-stop")
			default:
				continue
			}

			if err := beeep.Notify(title, text, image); err != nil {
				log.Println("beeep.Notify failed:", err)
			}
		case <-g.ctx.Done():
			return
		}
	}
}
