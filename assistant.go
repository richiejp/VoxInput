package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	openairt "github.com/WqyJh/go-openai-realtime/v2"
	"github.com/richiejp/VoxInput/internal/audio"
	"github.com/richiejp/VoxInput/internal/gui"
)

const functionNameDotool = "dotool"

func (l *Listener) startAssistantSession(ctx context.Context) error {
	voice := openairt.Voice("")
	if l.config.AssistantVoice != "" {
		voice = openairt.Voice(l.config.AssistantVoice)
	}

	var tools []openairt.ToolUnion
	if l.config.EnableDotool {
		tools = []openairt.ToolUnion{
			{
				Function: &openairt.ToolFunction{
					Name:        functionNameDotool,
					Description: "Execute dotool commands to control keyboard and mouse. Supports keyboard actions (key, keydown, keyup, type), mouse actions (click, buttondown, buttonup, wheel, hwheel, mouseto, mousemove), timing actions (keydelay, keyhold, typedelay, typehold), and sleep. Sleep takes milliseconds as argument.",
					Parameters: `{
						"type": "object",
						"properties": {
							"commands": {
								"type": "array",
								"description": "List of dotool commands to execute sequentially",
								"items": {
									"type": "string",
									"pattern": "^(key|keydown|keyup|type|click|buttondown|buttonup|wheel|hwheel|mouseto|mousemove|keydelay|keyhold|typedelay|typehold|sleep)\\s+.+$"
								}
							}
						},
						"required": ["commands"]
					}`,
				},
			},
		}
	}

	return l.conn.SendMessage(ctx, openairt.SessionUpdateEvent{
		EventBase: openairt.EventBase{
			EventID: "Initial update",
		},
		Session: openairt.SessionUnion{
			Realtime: &openairt.RealtimeSession{
				OutputModalities: []openairt.Modality{openairt.ModalityText, openairt.ModalityAudio},
				Instructions:     l.config.Instructions,
				Audio: &openairt.RealtimeSessionAudio{
					Input: &openairt.SessionAudioInput{
						Format: &openairt.AudioFormatUnion{
							PCM: &openairt.AudioFormatPCM{Rate: 24000},
						},
						TurnDetection: &openairt.TurnDetectionUnion{
							ServerVad: &openairt.ServerVad{},
						},
					},
					Output: &openairt.SessionAudioOutput{
						Voice: voice,
						Format: &openairt.AudioFormatUnion{
							PCM: &openairt.AudioFormatPCM{Rate: 24000},
						},
					},
				},
				Tools: tools,
			},
		},
	})
}

func (l *Listener) runAudioAssistant() {
	if err := audio.Duplex(l.ctx, l.playReader, l.chunkWriter, l.streamConfig); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
			return
		}
		l.errCh <- fmt.Errorf("audio duplex: %w", err)
		l.cancel()
	}
}

func (l *Listener) ReceiveAssistantMessages() {
	for {
		msg, err := l.conn.ReadMessage(l.ctx)
		if err != nil {
			var permanent *openairt.PermanentError
			if errors.As(err, &permanent) {
				log.Println("Listener.ReceiveAssistantMessages: Connection failed: ", err)
				l.cancel()
				return
			}
			log.Println("Listener.ReceiveAssistantMessages: error receiving message, retrying: ", err)
			continue
		}
		log.Println("Listener.ReceiveAssistantMessages: receiving message: ", msg.ServerEventType())
		switch msg.ServerEventType() {
		case openairt.ServerEventTypeInputAudioBufferSpeechStarted:
			l.config.UI.Chan <- &gui.ShowSpeechDetectedMsg{}
		case openairt.ServerEventTypeInputAudioBufferSpeechStopped:
			l.config.UI.Chan <- &gui.ShowSpeechSubmittedMsg{}
		case openairt.ServerEventTypeResponseCreated:
			l.config.UI.Chan <- &gui.ShowGeneratingResponseMsg{}
		case openairt.ServerEventTypeResponseOutputAudioDelta:
			delta := msg.(openairt.ResponseOutputAudioDeltaEvent)
			b, err := base64.StdEncoding.DecodeString(delta.Delta)
			if err != nil {
				log.Println("Listener.ReceiveAssistantMessages: error decoding audio delta: ", err)
				continue
			}
			select {
			case l.audioPlayChunks <- bytes.NewBuffer(b):
			default:
				log.Println("Listener.ReceiveAssistantMessages: dropped audio chunk")
			}
		case openairt.ServerEventTypeResponseFunctionCallArgumentsDone:
			event := msg.(openairt.ResponseFunctionCallArgumentsDoneEvent)
			l.config.UI.Chan <- &gui.ShowFunctionCallMsg{FunctionName: event.Name}
			if event.Name == functionNameDotool {
				var args struct {
					Commands []string `json:"commands"`
				}
				if err := json.Unmarshal([]byte(event.Arguments), &args); err != nil {
					log.Println("Listener.ReceiveAssistantMessages: error unmarshalling function arguments: ", err)
					continue
				}

				if err := l.executeDotoolCommands(args.Commands); err != nil {
					log.Println("Listener.ReceiveAssistantMessages: error executing dotool commands: ", err)
					continue
				}
			}
		case openairt.ServerEventTypeError:
			log.Println("Listener.ReceiveAssistantMessages: server error: ", msg.(openairt.ErrorEvent).Error.Message)
			continue
		default:
			continue
		}

	}
}

func (l *Listener) executeDotoolCommands(commands []string) error {
	dotool := exec.CommandContext(l.ctx, "dotool")
	stdin, err := dotool.StdinPipe()
	if err != nil {
		return fmt.Errorf("dotool stdin pipe: %w", err)
	}
	dotool.Stderr = os.Stderr
	if err := dotool.Start(); err != nil {
		return fmt.Errorf("dotool start: %w", err)
	}

	for _, cmd := range commands {
		parts := strings.SplitN(cmd, " ", 2)
		if len(parts) == 0 {
			continue
		}

		action := parts[0]
		if action == "sleep" {
			if len(parts) < 2 {
				return fmt.Errorf("sleep command requires milliseconds argument")
			}
			ms, err := strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return fmt.Errorf("invalid sleep duration: %w", err)
			}
			time.Sleep(time.Duration(ms) * time.Millisecond)
			continue
		}

		_, err = io.WriteString(stdin, cmd+"\n")
		if err != nil {
			return fmt.Errorf("dotool stdin WriteString: %w", err)
		}
	}

	if err := stdin.Close(); err != nil {
		return fmt.Errorf("close dotool stdin: %w", err)
	}
	if err := dotool.Wait(); err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return fmt.Errorf("dotool wait: %w", err)
	}
	return nil
}
