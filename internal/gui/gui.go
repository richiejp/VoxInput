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
type ShowStoppingMsg struct{}

func (m *ShowListeningMsg) IsMsg() bool { return true }
func (m *ShowStoppingMsg) IsMsg() bool { return true }

type GUI struct {
	a    fyne.App
	w    fyne.Window
	Chan chan Msg
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
	if g.w == nil {
		g.w = g.a.NewWindow("VoxInput")
		g.w.SetCloseIntercept(func() {
			g.w.Hide()
		})
		g.w.SetFixedSize(true)
		g.w.Resize(fyne.NewSize(300, 150))
	}

	status := widget.NewLabel(statusText)
	statusIcon := widget.NewIcon(icon)
	statusIcon.Resize(fyne.NewSize(100, 100))
	
	var ticks time.Duration
	tickTime := time.Millisecond * 50
	closeTimeout := 1500 * time.Millisecond
	countDown := widget.NewProgressBar()
	countDown.TextFormatter = func() string {
		return fmt.Sprintf("Closing in %.2fs", closeTimeout.Seconds()-ticks.Seconds())
	}

	g.w.SetContent(container.NewGridWithColumns(1,
		status,
		statusIcon,
		countDown,
	))

	go func() {
		ticker := time.NewTicker(tickTime)

		for range ticker.C {
			ticks += tickTime

			fyne.Do(func() {
				countDown.SetValue(float64(ticks) / float64(closeTimeout))

				if ticks > closeTimeout {
					g.w.Hide()
				}
			})

			if ticks > closeTimeout {
				return
			}
		}
	}()

	g.w.Show()
}
