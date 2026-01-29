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

	openairt "github.com/WqyJh/go-openai-realtime/v2"
	"github.com/richiejp/VoxInput/internal/audio"
	"github.com/richiejp/VoxInput/internal/gui"
)

const functionNameWriteText = "write_text"

func (l *Listener) startAssistantSession(ctx context.Context) error {
	voice := openairt.Voice("")
	if l.config.AssistantVoice != "" {
		voice = openairt.Voice(l.config.AssistantVoice)
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
				Tools: []openairt.ToolUnion{
					{
						Function: &openairt.ToolFunction{
							Name:        functionNameWriteText,
							Description: "Type text on the keyboard; when the user asks you to write or type something, you can use this function to do so",
							Parameters:  `{"type": "object", "properties": {"text": {"type": "string", "description": "The text to type"}}, "required": ["text"]}`,
						},
					},
				},
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
			if event.Name == functionNameWriteText {
				var args struct {
					Text string `json:"text"`
				}
				if err := json.Unmarshal([]byte(event.Arguments), &args); err != nil {
					log.Println("Listener.ReceiveAssistantMessages: error unmarshalling function arguments: ", err)
					continue
				}

				dotool := exec.CommandContext(l.ctx, "dotool")
				stdin, err := dotool.StdinPipe()
				if err != nil {
					l.errCh <- fmt.Errorf("dotool stdin pipe: %w", err)
					l.cancel()
					return
				}
				dotool.Stderr = os.Stderr
				if err := dotool.Start(); err != nil {
					l.errCh <- fmt.Errorf("dotool start: %w", err)
					l.cancel()
					return
				}
				_, err = io.WriteString(stdin, fmt.Sprintf("type %s ", args.Text))
				if err != nil {
					l.errCh <- fmt.Errorf("dotool stdin WriteString: %w", err)
					l.cancel()
					return
				}
				if err := stdin.Close(); err != nil {
					l.errCh <- fmt.Errorf("close dotool stdin: %w", err)
					l.cancel()
					return
				}
				if err := dotool.Wait(); err != nil {
					if errors.Is(err, context.Canceled) {
						return
					}
					l.errCh <- fmt.Errorf("dotool wait: %w", err)
					l.cancel()
					return
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
