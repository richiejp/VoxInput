package gui

import (
	"context"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/driver/desktop"
)

type Msg interface {
	IsMsg() bool
}

type ShowListeningMsg struct{}
type ShowSpeechDetectedMsg struct{}
type ShowTranscribingMsg struct{}
type ShowGeneratingResponseMsg struct{}
type HideMsg struct{}
type ShowStoppingMsg struct{}

func (m *ShowListeningMsg) IsMsg() bool      { return true }
func (m *ShowSpeechDetectedMsg) IsMsg() bool { return true }
func (m *ShowTranscribingMsg) IsMsg() bool   { return true }

func (m *ShowGeneratingResponseMsg) IsMsg() bool { return true }
func (m *HideMsg) IsMsg() bool                   { return true }
func (m *ShowStoppingMsg) IsMsg() bool           { return true }

type GUI struct {
	a                fyne.App
	Chan             chan Msg
	ListenIcon       fyne.Resource
	DetectedIcon     fyne.Resource
	TranscribingIcon fyne.Resource
	StopIcon         fyne.Resource
}

// TODO:: The App Icon does not work in the sys tray: https://github.com/fyne-io/fyne/issues/3968
func makeTray(ui GUI) {
	if desk, ok := ui.a.(desktop.App); ok {
		menu := fyne.NewMenu("VoxInput")
		desk.SetSystemTrayMenu(menu)
		desk.SetSystemTrayIcon(ui.TranscribingIcon)
	}
}

func New(ctx context.Context, showStatus string) *GUI {
	a := app.NewWithID("voxinput")

	ui := GUI{
		a:    a,
		Chan: make(chan Msg),
	}

	// Load icons
	if ListenIcon, err := fyne.LoadResourceFromPath("icons/microphone.png"); err != nil {
		panic(err)
	} else {
		ui.ListenIcon = ListenIcon
	}
	if DetectedIcon, err := fyne.LoadResourceFromPath("icons/play.png"); err != nil {
		panic(err)
	} else {
		ui.DetectedIcon = DetectedIcon
	}
	if TranscribingIcon, err := fyne.LoadResourceFromPath("icons/voice-note.png"); err != nil {
		panic(err)
	} else {
		ui.TranscribingIcon = TranscribingIcon
	}
	if StopIcon, err := fyne.LoadResourceFromPath("icons/pause.png"); err != nil {
		panic(err)
	} else {
		ui.StopIcon = StopIcon
	}

	a.SetIcon(ui.TranscribingIcon)

	makeTray(ui)

	go func() {
		for {
			select {
			case msg := <-ui.Chan:
				fyne.Do(func() {
					switch msg.(type) {
					case *ShowListeningMsg:
						if showStatus != "" {
							ui.showStatus("Listening with voice audio detection...", ui.ListenIcon)
						}
					case *ShowSpeechDetectedMsg:
						if showStatus != "" {
							ui.showStatus("Detected speech...", ui.DetectedIcon)
						}
					case *ShowTranscribingMsg:
						if showStatus != "" {
							ui.showStatus("Transcribing...", ui.TranscribingIcon)
						}
					case *ShowGeneratingResponseMsg:
						if showStatus != "" {
							ui.showStatus("Generating response...", ui.DetectedIcon)
						}
					case *ShowStoppingMsg:
						if showStatus != "" {
							ui.showStatus("Stopping listening", ui.StopIcon)
						}
					}
				})
			case <-ctx.Done():
				fyne.Do(func() {
					ui.a.Quit()
				})
				return
			}
		}
	}()

	return &ui
}

func (g *GUI) Run() {
	g.a.Run()
}

func (g *GUI) showStatus(statusText string, icon fyne.Resource) {
	if desk, ok := g.a.(desktop.App); ok {
		m := fyne.NewMenu("VoxInput",
			fyne.NewMenuItem(statusText, nil),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Quit", func() {
				g.a.Quit()
			}),
		)
		desk.SetSystemTrayIcon(icon)
		desk.SetSystemTrayMenu(m)
	}
}
