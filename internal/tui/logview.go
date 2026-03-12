package tui

import (
	"fmt"
	"time"

	"github.com/richiejp/VoxInput/internal/ipc"
)

func renderLogEvent(e ipc.Event) string {
	ts := time.UnixMilli(e.Ts).Format("15:04:05.000")
	return fmt.Sprintf("%s %s", logTimestampStyle.Render(ts), e.Text)
}

func renderStderrLine(text string) string {
	ts := time.Now().Format("15:04:05.000")
	return fmt.Sprintf("%s %s", logTimestampStyle.Render(ts), text)
}
