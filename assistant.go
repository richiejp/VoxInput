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
	"net/http"
	"os"
	"os/exec"
	"strings"

	openairt "github.com/WqyJh/go-openai-realtime/v2"
	"github.com/sashabaranov/go-openai/jsonschema"
	"github.com/richiejp/VoxInput/internal/audio"
	"github.com/richiejp/VoxInput/internal/gui"
	"github.com/richiejp/VoxInput/internal/input"
)

const functionNameInputControl = "input_control"
const functionNameTakeScreenshot = "take_screenshot"

func (l *Listener) startAssistantSession(ctx context.Context) error {
	voice := openairt.Voice("")
	if l.config.AssistantVoice != "" {
		voice = openairt.Voice(l.config.AssistantVoice)
	}

	var tools []openairt.ToolUnion
	if l.config.EnableDotool {
		schema, err := jsonschema.GenerateSchemaForType(input.CommandParameters{})
		if err != nil {
			return fmt.Errorf("generate input control schema: %w", err)
		}

		schemaJSON, err := json.MarshalIndent(schema, "", "  ")
		if err != nil {
			log.Printf("Failed to marshal schema: %v", err)
		} else {
			log.Printf("Generated schema:\n%s", schemaJSON)
		}

		tools = append(tools, openairt.ToolUnion{
			Function: &openairt.ToolFunction{
				Name:        functionNameInputControl,
				Description: "Execute input commands to control keyboard and mouse. Supports keyboard actions (key, keydown, keyup, type), mouse actions (click, buttondown, buttonup, wheel, hwheel, mouseto, mousemove), timing actions (keydelay, keyhold, typedelay, typehold), and sleep. Sleep takes milliseconds as argument.",
				Parameters:  schema,
			},
		})
	}

	if l.config.ScreenshotCommand != "" && l.config.ScreenshotFile != "" {
		screenshotSchema, err := jsonschema.GenerateSchemaForType(input.ScreenshotParameters{})
		if err != nil {
			return fmt.Errorf("generate screenshot schema: %w", err)
		}

		tools = append(tools, openairt.ToolUnion{
			Function: &openairt.ToolFunction{
				Name:        functionNameTakeScreenshot,
				Description: "Take a screenshot of the desktop. The screenshot will be added to the conversation as an image.",
				Parameters:  screenshotSchema,
			},
		})
	}

	var transcription *openairt.AudioTranscription
	if l.config.Model != "" {
		transcription = &openairt.AudioTranscription{
			Model:    l.config.Model,
			Language: l.config.Lang,
			Prompt:   l.config.Prompt,
		}
	}

	return l.conn.SendMessage(ctx, openairt.SessionUpdateEvent{
		EventBase: openairt.EventBase{
			EventID: "Initial update",
		},
		Session: openairt.SessionUnion{
			Realtime: &openairt.RealtimeSession{
				Instructions:     l.config.Instructions,
				Audio: &openairt.RealtimeSessionAudio{
					Input: &openairt.SessionAudioInput{
						Format: &openairt.AudioFormatUnion{
							PCM: &openairt.AudioFormatPCM{Rate: l.config.InputSampleRate},
						},
						Transcription: transcription,
						TurnDetection: &openairt.TurnDetectionUnion{
							ServerVad: &openairt.ServerVad{},
						},
					},
					Output: &openairt.SessionAudioOutput{
						Voice: voice,
						Format: &openairt.AudioFormatUnion{
							PCM: &openairt.AudioFormatPCM{Rate: l.config.OutputSampleRate},
						},
					},
				},
				Tools: tools,
			},
		},
	})
}

func (l *Listener) runAudioAssistant() {
	if err := audio.Duplex(l.ctx, l.playReader, l.chunkWriter, l.streamConfig, l.echoCanceller, l.duplexOpts); err != nil {
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
			log.Println("Listener.ReceiveAssistantMessages: speech detected")
			l.config.UI.Chan <- &gui.ShowSpeechDetectedMsg{}
		case openairt.ServerEventTypeInputAudioBufferSpeechStopped:
			log.Println("Listener.ReceiveAssistantMessages: speech stopped, processing")
			l.config.UI.Chan <- &gui.ShowSpeechSubmittedMsg{}
		case openairt.ServerEventTypeResponseCreated:
			log.Println("Listener.ReceiveAssistantMessages: generating response")
			l.config.UI.Chan <- &gui.ShowGeneratingResponseMsg{}
		case openairt.ServerEventTypeConversationItemInputAudioTranscriptionCompleted:
			transcript := msg.(openairt.ConversationItemInputAudioTranscriptionCompletedEvent).Transcript
			log.Printf("Listener.ReceiveAssistantMessages: user said: %s", transcript)
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
			log.Printf("Listener.ReceiveAssistantMessages: function call %s with arguments: %s", event.Name, event.Arguments)
			l.config.UI.Chan <- &gui.ShowFunctionCallMsg{
				FunctionName: event.Name,
				Arguments:    event.Arguments,
			}
			switch event.Name {
			case functionNameInputControl:
				var args input.CommandParameters
				if err := json.Unmarshal([]byte(event.Arguments), &args); err != nil {
					log.Println("Listener.ReceiveAssistantMessages: error unmarshalling function arguments: ", err)
					continue
				}

				if err := l.config.InputController.ExecuteCommands(l.ctx, args.Commands); err != nil {
					log.Println("Listener.ReceiveAssistantMessages: error executing input commands: ", err)
					continue
				}

				if err := l.conn.SendMessage(l.ctx, openairt.ConversationItemCreateEvent{
					Item: openairt.MessageItemUnion{
						FunctionCallOutput: &openairt.MessageItemFunctionCallOutput{
							CallID: event.CallID,
							Output: "Completed: " + args.Summary,
						},
					},
				}); err != nil {
					log.Println("Listener.ReceiveAssistantMessages: error sending function call output: ", err)
					continue
				}
			case functionNameTakeScreenshot:
				var ssArgs input.ScreenshotParameters
				if err := json.Unmarshal([]byte(event.Arguments), &ssArgs); err != nil {
					log.Println("Listener.ReceiveAssistantMessages: error unmarshalling screenshot arguments: ", err)
				}
				if err := l.takeScreenshot(event.CallID, ssArgs.Reason); err != nil {
					log.Println("Listener.ReceiveAssistantMessages: error taking screenshot: ", err)
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

func (l *Listener) takeScreenshot(callID string, reason string) error {
	log.Printf("Listener.takeScreenshot: %s", reason)

	args := strings.Fields(l.config.ScreenshotCommand)
	cmd := exec.CommandContext(l.ctx, args[0], args[1:]...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("screenshot command: %w", err)
	}

	imgData, err := os.ReadFile(l.config.ScreenshotFile)
	if err != nil {
		return fmt.Errorf("read screenshot file: %w", err)
	}

	mimeType := http.DetectContentType(imgData)
	if !strings.HasPrefix(mimeType, "image/") {
		mimeType = "image/png"
	}
	dataURI := fmt.Sprintf("data:%s;base64,%s",
		mimeType,
		base64.StdEncoding.EncodeToString(imgData),
	)

	log.Printf("Listener.takeScreenshot: captured %d bytes, sending as %s", len(imgData), mimeType)

	if err := l.conn.SendMessage(l.ctx, openairt.ConversationItemCreateEvent{
		Item: openairt.MessageItemUnion{
			FunctionCallOutput: &openairt.MessageItemFunctionCallOutput{
				CallID: callID,
				Output: "Screenshot captured: " + reason,
			},
		},
	}); err != nil {
		return fmt.Errorf("send function call output: %w", err)
	}

	if err := l.conn.SendMessage(l.ctx, openairt.ConversationItemCreateEvent{
		Item: openairt.MessageItemUnion{
			User: &openairt.MessageItemUser{
				Content: []openairt.MessageContentInput{
					{
						Type:     openairt.MessageContentType("input_image"),
						ImageURL: dataURI,
						Detail:   openairt.ImageDetailAuto,
					},
					{
						Type: openairt.MessageContentTypeInputText,
						Text: "Image is a screenshot of the desktop",
					},
				},
			},
		},
	}); err != nil {
		return fmt.Errorf("send screenshot image: %w", err)
	}

	if err := l.conn.SendMessage(l.ctx, openairt.ResponseCreateEvent{}); err != nil {
		return fmt.Errorf("trigger response after screenshot: %w", err)
	}

	log.Println("Listener.takeScreenshot: screenshot sent to conversation")
	return nil
}

