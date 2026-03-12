package tui

import (
	"fmt"
	"time"

	"github.com/richiejp/VoxInput/internal/ipc"
)

func renderChatEvent(e ipc.Event) string {
	ts := time.UnixMilli(e.Ts).Format("15:04:05")

	switch e.Kind {
	case ipc.EventTranscript:
		if e.IsUser {
			return fmt.Sprintf("%s %s %s",
				logTimestampStyle.Render(ts),
				userStyle.Render("You:"),
				e.Text)
		}
		return fmt.Sprintf("%s %s %s",
			logTimestampStyle.Render(ts),
			assistantStyle.Render("Assistant:"),
			e.Text)

	case ipc.EventStatus:
		if e.Text == "" {
			return ""
		}
		return statusStyle.Render(fmt.Sprintf("%s %s", ts, e.Text))

	case ipc.EventFunctionCall:
		content := e.Text
		if e.Detail != "" {
			content += "\n" + e.Detail
		}
		return functionCallStyle.Render(fmt.Sprintf("%s %s", ts, content))

	case ipc.EventError:
		return errorStyle.Render(fmt.Sprintf("%s ERROR: %s", ts, e.Text))

	default:
		return fmt.Sprintf("%s %s", ts, e.Text)
	}
}
