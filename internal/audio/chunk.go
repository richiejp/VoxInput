package audio

import (
	"bytes"
	"context"
	"sync"
	"time"
)

// ChunkWriter buffers Write calls into a *bytes.Buffer and ships it onto a
// channel every 250 ms (or on explicit Flush). Used to coalesce realtime
// captured audio into chunks for upstream encoding/transport.
type ChunkWriter struct {
	ctx      context.Context
	ready    chan<- (*bytes.Buffer)
	current  *bytes.Buffer
	lastSend time.Time
}

func NewChunkWriter(ctx context.Context, ready chan<- (*bytes.Buffer)) *ChunkWriter {
	return &ChunkWriter{
		ctx:      ctx,
		ready:    ready,
		current:  new(bytes.Buffer),
		lastSend: time.Now(),
	}
}

func (w *ChunkWriter) Flush() {
	if w.current.Len() > 0 {
		select {
		case w.ready <- w.current:
		case <-w.ctx.Done():
		}
		w.current = new(bytes.Buffer)
	}
}

func (w *ChunkWriter) Write(p []byte) (n int, err error) {
	now := time.Now()
	if now.Sub(w.lastSend) >= 250*time.Millisecond {
		select {
		case w.ready <- w.current:
		case <-w.ctx.Done():
			return 0, w.ctx.Err()
		}
		w.current = new(bytes.Buffer)
		w.lastSend = now
	}

	return w.current.Write(p)
}

// ChunkReader exposes a channel of *bytes.Buffer chunks as an io.Reader,
// with a small playout jitter buffer so realtime audio callbacks (which
// fire on a hard cadence) don't see silence whenever a callback beats the
// next chunk arrival. Read returns 0 (silence) until PrerollBytes worth of
// chunks have queued, then drains; on underrun it re-enters the buffering
// state and waits for another preroll's worth before unblocking.
type ChunkReader struct {
	ctx          context.Context
	chunks       <-chan *bytes.Buffer
	mu           sync.Mutex
	current      *bytes.Buffer
	pending      []*bytes.Buffer
	pendingBytes int
	prerollBytes int
	buffering    bool
}

// NewChunkReader returns a reader over chunks. prerollBytes sets the playout
// jitter buffer threshold; pass 0 to disable buffering (Read serves whatever
// is available immediately).
func NewChunkReader(ctx context.Context, chunks <-chan *bytes.Buffer, prerollBytes int) *ChunkReader {
	return &ChunkReader{
		ctx:          ctx,
		chunks:       chunks,
		prerollBytes: prerollBytes,
		buffering:    true,
	}
}

// drainAvailable pulls every chunk currently sitting in the channel into the
// pending queue so Read can decide based on total buffered bytes (peeking
// the channel itself isn't possible).
func (r *ChunkReader) drainAvailable() {
	for {
		select {
		case buf, ok := <-r.chunks:
			if !ok {
				return
			}
			r.pending = append(r.pending, buf)
			r.pendingBytes += buf.Len()
		default:
			return
		}
	}
}

// Flush discards all buffered and queued audio, returning the reader to the
// pre-roll buffering state. Used for barge-in: when the user starts speaking
// while the assistant is talking, the already-queued TTS audio must be dropped
// so playback stops immediately rather than draining to the end of the
// response. Safe to call concurrently with Read.
func (r *ChunkReader) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.drainAvailable()
	r.current = nil
	r.pending = nil
	r.pendingBytes = 0
	r.buffering = true
}

func (r *ChunkReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.drainAvailable()

	if r.buffering {
		if r.pendingBytes < r.prerollBytes {
			return 0, nil
		}
		r.buffering = false
	}

	n := 0
	for len(p) > 0 {
		if r.current == nil || r.current.Len() == 0 {
			if len(r.pending) == 0 {
				r.buffering = true
				return n, nil
			}
			r.current = r.pending[0]
			r.pending = r.pending[1:]
		}

		nn, _ := r.current.Read(p)
		n += nn
		p = p[nn:]
		r.pendingBytes -= nn
		if r.current.Len() == 0 {
			r.current = nil
		}
	}
	return n, nil
}
