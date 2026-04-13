//go:build linux

package input

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type dotoolController struct{}

func New() (Controller, error) {
	return &dotoolController{}, nil
}

func (c *dotoolController) ExecuteCommands(ctx context.Context, commands []Command) error {
	log.Printf("dotoolController.ExecuteCommands: executing %d commands", len(commands))
	dotool := exec.CommandContext(ctx, "dotool")
	stdin, err := dotool.StdinPipe()
	if err != nil {
		return fmt.Errorf("dotool stdin pipe: %w", err)
	}
	dotool.Stderr = os.Stderr
	if err := dotool.Start(); err != nil {
		return fmt.Errorf("dotool start: %w", err)
	}

	for i, cmd := range commands {
		log.Printf("dotoolController.ExecuteCommands: [%d/%d] %s %s", i+1, len(commands), cmd.Action, cmd.Args)

		if cmd.Action == "sleep" {
			ms, err := strconv.Atoi(strings.TrimSpace(cmd.Args))
			if err != nil {
				return fmt.Errorf("invalid sleep duration: %w", err)
			}
			time.Sleep(time.Duration(ms) * time.Millisecond)
			continue
		}

		cmdLine := cmd.Action
		if cmd.Args != "" {
			cmdLine += " " + cmd.Args
		}
		_, err = io.WriteString(stdin, cmdLine+"\n")
		if err != nil {
			return fmt.Errorf("dotool stdin WriteString: %w", err)
		}
	}

	if err := stdin.Close(); err != nil {
		return fmt.Errorf("close dotool stdin: %w", err)
	}
	if err := dotool.Wait(); err != nil {
		return fmt.Errorf("dotool wait: %w", err)
	}
	log.Println("dotoolController.ExecuteCommands: completed successfully")
	return nil
}

func (c *dotoolController) TypeText(ctx context.Context, text string) error {
	// Replace newlines and surrounding whitespace with a single space
	// to prevent dotool from interpreting newlines as command separators
	sanitizedText := strings.Join(strings.Fields(text), " ")
	return c.ExecuteCommands(ctx, []Command{{Action: "type", Args: sanitizedText}})
}

func (c *dotoolController) Close() error {
	return nil
}
