package gui

import (
	"context"
	"testing"
	"time"
)

func TestGUISendReceive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	g := New(ctx, false)

	msgs := []Msg{
		&ShowListeningMsg{},
		&ShowSpeechDetectedMsg{},
		&ShowTranscribingMsg{},
		&ShowSpeechSubmittedMsg{},
		&ShowGeneratingResponseMsg{},
		&ShowFunctionCallMsg{FunctionName: "test", Arguments: "args"},
		&HideMsg{},
		&ShowStoppingMsg{},
		&ShowTranscriptMsg{Text: "hello", IsUser: true},
	}

	go g.Run()

	for _, msg := range msgs {
		g.Send(msg)
	}

	// Give the Run goroutine time to consume messages
	time.Sleep(50 * time.Millisecond)
	cancel()

	if len(g.Chan) != 0 {
		t.Errorf("expected channel to be drained, got %d messages", len(g.Chan))
	}
}

func TestGUIShowStatusFalse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	g := New(ctx, false)
	go g.Run()

	g.Send(&ShowListeningMsg{})
	time.Sleep(50 * time.Millisecond)
	cancel()
	// Should not panic or error with showStatus=false
}

func TestGUIContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	g := New(ctx, false)

	done := make(chan struct{})
	go func() {
		g.Run()
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after context cancel")
	}
}

func TestStatusSinkInterface(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := New(ctx, false)

	var sink StatusSink = g
	sink.Send(&ShowListeningMsg{})

	select {
	case msg := <-g.Chan:
		if _, ok := msg.(*ShowListeningMsg); !ok {
			t.Errorf("unexpected message type: %T", msg)
		}
	default:
		t.Error("expected message on channel")
	}
}

func TestMultiSinkFanout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g1 := New(ctx, false)
	g2 := New(ctx, false)

	ms := &MultiSink{Sinks: []StatusSink{g1, g2}}
	ms.Send(&ShowListeningMsg{})

	select {
	case <-g1.Chan:
	default:
		t.Error("g1 did not receive message")
	}

	select {
	case <-g2.Chan:
	default:
		t.Error("g2 did not receive message")
	}
}
