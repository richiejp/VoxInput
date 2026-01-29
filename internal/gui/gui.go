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
}
type HideMsg struct{}
type ShowStoppingMsg struct{}

func (m *ShowListeningMsg) IsMsg() bool          { return true }
func (m *ShowSpeechDetectedMsg) IsMsg() bool     { return true }
func (m *ShowTranscribingMsg) IsMsg() bool       { return true }
func (m *ShowSpeechSubmittedMsg) IsMsg() bool    { return true }
func (m *ShowGeneratingResponseMsg) IsMsg() bool { return true }
func (m *ShowFunctionCallMsg) IsMsg() bool       { return true }
func (m *HideMsg) IsMsg() bool                   { return true }
func (m *ShowStoppingMsg) IsMsg() bool           { return true }

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

func (g *GUI) Run() {
	for {
		select {
		case msg := <-g.Chan:
			if !g.showStatus {
				continue
			}

			title := "VoxInput"
			var text string
			image := "audio-input-microphone"

			switch msg.(type) {
			case *ShowListeningMsg:
				text = "Listening with voice audio detection..."
				image = "audio-input-microphone"
			case *ShowSpeechDetectedMsg:
				text = "Detected speech..."
				image = "audio-input-microphone"
			case *ShowTranscribingMsg:
				text = "Transcribing..."
				image = "text-x-generic"
			case *ShowSpeechSubmittedMsg:
				text = "Speech submitted..."
				image = "network-transmit-receive"
			case *ShowGeneratingResponseMsg:
				text = "Generating response..."
				image = "audio-speakers"
			case *ShowFunctionCallMsg:
				text = "Calling function " + msg.(*ShowFunctionCallMsg).FunctionName + "..."
				image = "applications-system"
			case *HideMsg:
				continue
			case *ShowStoppingMsg:
				text = "Stopping listening"
				image = "media-playback-stop"
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
