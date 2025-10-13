package gui

import (
	"context"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type Msg interface {
	IsMsg() bool
}

type ShowListeningMsg struct{}
type ShowSpeechDetectedMsg struct{}
type ShowTranscribingMsg struct{}
type HideMsg struct{}
type ShowStoppingMsg struct{}

func (m *ShowListeningMsg) IsMsg() bool      { return true }
func (m *ShowSpeechDetectedMsg) IsMsg() bool { return true }
func (m *ShowTranscribingMsg) IsMsg() bool   { return true }
func (m *HideMsg) IsMsg() bool               { return true }
func (m *ShowStoppingMsg) IsMsg() bool       { return true }

type GUI struct {
	a           fyne.App
	w           fyne.Window
	Chan        chan Msg
	cancelTimer context.CancelFunc
	timerCtx    context.Context
	statusLabel *widget.Label
	statusIcon  *widget.Icon
	countDown   *widget.ProgressBar
}

// TODO:: The App Icon does not work in the sys tray: https://github.com/fyne-io/fyne/issues/3968
func makeTray(a fyne.App) {
	if desk, ok := a.(desktop.App); ok {
		menu := fyne.NewMenu("VoxInput")
		desk.SetSystemTrayMenu(menu)
	}
}

func New(ctx context.Context, showStatus string) *GUI {
	a := app.NewWithID("voxinput")
	a.SetIcon(theme.MediaRecordIcon())

	ui := GUI{
		a:    a,
		Chan: make(chan Msg),
	}

	makeTray(a)

	go func() {
		for {
			select {
			case msg := <-ui.Chan:
				fyne.Do(func() {
					switch msg.(type) {
					case *ShowListeningMsg:
						if showStatus != "" {
							ui.showStatus("Listening with voice audio detection...", theme.MediaRecordIcon())
						}
					case *ShowSpeechDetectedMsg:
						if showStatus != "" {
							ui.showStatus("Detected speech...", theme.MediaMusicIcon())
						}
					case *ShowTranscribingMsg:
						if showStatus != "" {
							ui.showStatus("Transcribing...", theme.FileTextIcon())
						}
					case *HideMsg:
						if ui.cancelTimer != nil {
							ui.cancelTimer()
						}
						if ui.w != nil {
							ui.w.Hide()
						}
					case *ShowStoppingMsg:
						if showStatus != "" {
							ui.showStatus("Stopping listening", theme.MediaStopIcon())
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
	// Cancel any existing timer goroutine
	if g.cancelTimer != nil {
		g.cancelTimer()
	}

	// Create window and widgets only once
	if g.w == nil {
		g.w = g.a.NewWindow("VoxInput")
		g.w.SetFixedSize(true)
		g.w.Resize(fyne.NewSize(300, 150))

		g.statusLabel = widget.NewLabel(statusText)
		g.statusIcon = widget.NewIcon(icon)
		g.statusIcon.Resize(fyne.NewSize(100, 100))

		g.countDown = widget.NewProgressBar()
		g.countDown.TextFormatter = func() string {
			return ""
		}

		g.w.SetContent(container.NewGridWithColumns(1,
			g.statusLabel,
			g.statusIcon,
			g.countDown,
		))
	} else {
		// Update existing widgets
		g.statusLabel.SetText(statusText)
		g.statusIcon.SetResource(icon)
	}

	var ticks time.Duration
	tickTime := time.Millisecond * 50
	closeTimeout := 1500 * time.Millisecond

	// Update formatter with fresh state
	g.countDown.TextFormatter = func() string {
		return fmt.Sprintf("Closing in %.2fs", closeTimeout.Seconds()-ticks.Seconds())
	}
	g.countDown.SetValue(0)
	g.countDown.Refresh()

	// Create a new context for this timer
	g.timerCtx, g.cancelTimer = context.WithCancel(context.Background())
	timerCtx := g.timerCtx

	go func() {
		ticker := time.NewTicker(tickTime)
		defer ticker.Stop()

		for {
			select {
			case <-timerCtx.Done():
				// Timer was cancelled, exit goroutine
				return
			case <-ticker.C:
				ticks += tickTime

				fyne.Do(func() {
					g.countDown.SetValue(float64(ticks) / float64(closeTimeout))

					if ticks > closeTimeout {
						g.w.Hide()
					}
				})

				if ticks > closeTimeout {
					return
				}
			}
		}
	}()

	g.w.Show()
}
