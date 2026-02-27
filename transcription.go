package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	openairt "github.com/WqyJh/go-openai-realtime/v2"
	"github.com/richiejp/VoxInput/internal/audio"
	"github.com/richiejp/VoxInput/internal/gui"
)

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
			l.config.UI.Chan <- &gui.ShowSpeechDetectedMsg{}
		case openairt.ServerEventTypeInputAudioBufferSpeechStopped:
			log.Println("Listener.ReceiveTranscriptionMessages: speech stopped, transcribing")
			l.config.UI.Chan <- &gui.ShowTranscribingMsg{}
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
		l.config.UI.Chan <- &gui.HideMsg{}
		log.Println("Listener.ReceiveTranscriptionMessages: received transcribed text: ", text)
		if l.config.OutputFile != "" {
			f, err := os.OpenFile(l.config.OutputFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				log.Printf("Failed to open output file %s: %v\n", l.config.OutputFile, err)
				continue
			}
			if _, err := fmt.Fprintln(f, text); err != nil {
				log.Printf("Failed to write to output file: %v\n", err)
			}
			if err := f.Close(); err != nil {
				log.Printf("Failed to close output file: %v\n", err)
			}
			continue
		}
		log.Printf("Listener.ReceiveTranscriptionMessages: typing text: %q", text)
		if l.config.InputController == nil {
			log.Println("Listener.ReceiveTranscriptionMessages: no input controller available, cannot type text")
			continue
		}
		if err := l.config.InputController.TypeText(l.ctx, text); err != nil {
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
