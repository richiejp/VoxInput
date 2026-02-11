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
	"github.com/invopop/jsonschema"
	"github.com/richiejp/VoxInput/internal/audio"
	"github.com/richiejp/VoxInput/internal/gui"
)

const functionNameDotool = "dotool"

type dotoolCommand struct {
	Action string `json:"action" jsonschema:"required,description=The dotool action to perform,enum=key,enum=keydown,enum=keyup,enum=type,enum=click,enum=buttondown,enum=buttonup,enum=wheel,enum=hwheel,enum=mouseto,enum=mousemove,enum=keydelay,enum=keyhold,enum=typedelay,enum=typehold,enum=sleep"`
	Args   string `json:"args" jsonschema:"required,description=Arguments for the action"`
}

type dotoolParameters struct {
	Commands []dotoolCommand `json:"commands" jsonschema:"required,description=List of dotool commands to execute sequentially"`
}

func (l *Listener) startAssistantSession(ctx context.Context) error {
	voice := openairt.Voice("")
	if l.config.AssistantVoice != "" {
		voice = openairt.Voice(l.config.AssistantVoice)
	}

	var tools []openairt.ToolUnion
	if l.config.EnableDotool {
		reflector := jsonschema.Reflector{}
		schema := reflector.Reflect(&dotoolParameters{})
		
		schemaJSON, err := json.MarshalIndent(schema, "", "  ")
		if err != nil {
			log.Printf("Failed to marshal schema: %v", err)
		} else {
			log.Printf("Generated schema:\n%s", schemaJSON)
		}
		
		// Ensure type is set to "object" for OpenAI compatibility
		schema.Type = "object"

		tools = []openairt.ToolUnion{
			{
				Function: &openairt.ToolFunction{
					Name:        functionNameDotool,
					Description: "Execute dotool commands to control keyboard and mouse. Supports keyboard actions (key, keydown, keyup, type), mouse actions (click, buttondown, buttonup, wheel, hwheel, mouseto, mousemove), timing actions (keydelay, keyhold, typedelay, typehold), and sleep. Sleep takes milliseconds as argument.",
					Parameters:  schema,
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
				Instructions:     l.config.Instructions,
				Audio: &openairt.RealtimeSessionAudio{
					Input: &openairt.SessionAudioInput{
						Format: &openairt.AudioFormatUnion{
							PCM: &openairt.AudioFormatPCM{Rate: l.config.InputSampleRate},
						},
						Transcription: &openairt.AudioTranscription{
							Model:    l.config.Model,
							Language: l.config.Lang,
							Prompt:   l.config.Prompt,
						},
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
			if event.Name == functionNameDotool {
				var args dotoolParameters
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

func (l *Listener) executeDotoolCommands(commands []dotoolCommand) error {
	log.Printf("Listener.executeDotoolCommands: executing %d commands", len(commands))
	dotool := exec.CommandContext(l.ctx, "dotool")
	stdin, err := dotool.StdinPipe()
	if err != nil {
		return fmt.Errorf("dotool stdin pipe: %w", err)
	}
	dotool.Stderr = os.Stderr
	if err := dotool.Start(); err != nil {
		return fmt.Errorf("dotool start: %w", err)
	}

	for i, cmd := range commands {
		log.Printf("Listener.executeDotoolCommands: [%d/%d] %s %s", i+1, len(commands), cmd.Action, cmd.Args)

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
		if errors.Is(err, context.Canceled) {
			return err
		}
		return fmt.Errorf("dotool wait: %w", err)
	}
	log.Println("Listener.executeDotoolCommands: completed successfully")
	return nil
}
