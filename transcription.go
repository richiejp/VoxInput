package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	openairt "github.com/WqyJh/go-openai-realtime/v2"
	"github.com/richiejp/VoxInput/internal/audio"
	"github.com/richiejp/VoxInput/internal/gui"
	"github.com/sashabaranov/go-openai"
)

// translate sends a transcript chunk to the chat completions API and returns
// the model's translation. The system prompt is configurable; it defaults to a
// literal English translation with minimal explanation.
func (l *Listener) translate(ctx context.Context, text string) (string, error) {
	resp, err := l.chatCli.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: l.config.TranslationModel,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: l.config.TranslationInstructions,
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: text,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("translation chat completion: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("translation returned no choices")
	}

	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

func (l *Listener) startTranscriptionSession(ctx context.Context) error {
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
			Transcription: &openairt.TranscriptionSession{
				Audio: &openairt.TranscriptionSessionAudio{
					Input: &openairt.SessionAudioInput{
						Transcription: transcription,
						TurnDetection: &openairt.TurnDetectionUnion{
							ServerVad: &openairt.ServerVad{},
						},
					},
				},
			},
		},
	})
}

func (l *Listener) runAudioTranscription() {
	if err := audio.Capture(l.ctx, l.chunkWriter, l.streamConfig); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		l.errCh <- fmt.Errorf("audio capture: %w", err)
		l.cancel()
	}
}

func (l *Listener) ReceiveTranscriptionMessages() {
	for {
		msg, err := l.conn.ReadMessage(l.ctx)
		if err != nil {
			var permanent *openairt.PermanentError
			if errors.As(err, &permanent) {
				log.Println("Listener.ReceiveTranscriptionMessages: Connection failed: ", err)
				l.cancel()
				return
			}
			log.Println("Listener.ReceiveTranscriptionMessages: error receiving message, retrying: ", err)
			continue
		}
		log.Println("Listener.ReceiveTranscriptionMessages: receiving message: ", msg.ServerEventType())
		var text string
		switch msg.ServerEventType() {
		case openairt.ServerEventTypeInputAudioBufferSpeechStarted:
			log.Println("Listener.ReceiveTranscriptionMessages: speech detected")
			l.config.UI.Send(&gui.ShowSpeechDetectedMsg{})
		case openairt.ServerEventTypeInputAudioBufferSpeechStopped:
			log.Println("Listener.ReceiveTranscriptionMessages: speech stopped, transcribing")
			l.config.UI.Send(&gui.ShowTranscribingMsg{})
		case openairt.ServerEventTypeResponseOutputAudioTranscriptDone:
			text = msg.(openairt.ResponseOutputAudioTranscriptDoneEvent).Transcript
		case openairt.ServerEventTypeConversationItemInputAudioTranscriptionCompleted:
			text = msg.(openairt.ConversationItemInputAudioTranscriptionCompletedEvent).Transcript
		case openairt.ServerEventTypeError:
			log.Println("Listener.ReceiveTranscriptionMessages: server error: ", msg.(openairt.ErrorEvent).Error.Message)
			continue
		default:
			continue
		}
		if text == "" {
			continue
		}
		// In translation mode the raw transcript is shown as the "Original"
		// line and the translation as the "Translation" line; otherwise the
		// transcript is the user's own speech ("You").
		translating := l.config.Mode == "translation"
		userMsg := &gui.ShowTranscriptMsg{Text: text, IsUser: true}
		if translating {
			userMsg.Label = "Original"
		}
		l.config.UI.Send(&gui.HideMsg{})
		l.config.UI.Send(userMsg)
		log.Println("Listener.ReceiveTranscriptionMessages: received transcribed text: ", text)

		// In translation mode the raw transcript is handed to the chat
		// completions API and the translation replaces it for output.
		out := text
		if translating {
			l.config.UI.Send(&gui.ShowGeneratingResponseMsg{})
			translated, err := l.translate(l.ctx, text)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				log.Println("Listener.ReceiveTranscriptionMessages: translation failed: ", err)
				continue
			}
			log.Printf("Listener.ReceiveTranscriptionMessages: translated text: %q", translated)
			l.config.UI.Send(&gui.HideMsg{})
			l.config.UI.Send(&gui.ShowTranscriptMsg{Text: translated, IsUser: false, Label: "Translation"})
			out = translated
		}

		if l.config.OutputFile != "" {
			f, err := os.OpenFile(l.config.OutputFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				log.Printf("Failed to open output file %s: %v\n", l.config.OutputFile, err)
				continue
			}
			if _, err := fmt.Fprintln(f, out); err != nil {
				log.Printf("Failed to write to output file: %v\n", err)
			}
			if err := f.Close(); err != nil {
				log.Printf("Failed to close output file: %v\n", err)
			}
			continue
		}
		log.Printf("Listener.ReceiveTranscriptionMessages: typing text: %q", out)
		if l.config.InputController == nil {
			log.Println("Listener.ReceiveTranscriptionMessages: no input controller available, cannot type text")
			continue
		}
		if err := l.config.InputController.TypeText(l.ctx, out); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			l.errCh <- fmt.Errorf("type text: %w", err)
			l.cancel()
			return
		}
		log.Println("Listener.ReceiveTranscriptionMessages: text typed successfully")
	}
}
