package main

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

// --- chunkWriter tests ---

func TestChunkWriter_FlushesAfter250ms(t *testing.T) {
	ctx := context.Background()
	ch := make(chan *bytes.Buffer, 10)
	w := newChunkWriter(ctx, ch)

	data := []byte("hello")
	w.Write(data)

	select {
	case <-ch:
		t.Fatal("should not flush before 250ms")
	default:
	}

	time.Sleep(260 * time.Millisecond)

	// Next write triggers the flush of the previous buffer
	w.Write([]byte("world"))

	select {
	case buf := <-ch:
		if buf.String() != "hello" {
			t.Errorf("flushed buffer: got %q, want %q", buf.String(), "hello")
		}
	default:
		t.Fatal("expected flush after 250ms + write")
	}
}

func TestChunkWriter_Flush(t *testing.T) {
	ctx := context.Background()
	ch := make(chan *bytes.Buffer, 10)
	w := newChunkWriter(ctx, ch)

	// Write 100 bytes, wait for flush
	first := make([]byte, 100)
	for i := range first {
		first[i] = 0xAA
	}
	w.Write(first)
	time.Sleep(260 * time.Millisecond)

	// This write triggers flush of the first 100 bytes
	second := make([]byte, 50)
	for i := range second {
		second[i] = 0xBB
	}
	w.Write(second)

	// Explicit flush sends the remaining 50 bytes
	w.Flush()

	// Drain channel
	totalReceived := 0
	for {
		select {
		case buf := <-ch:
			totalReceived += buf.Len()
		default:
			goto done
		}
	}
done:
	totalWritten := len(first) + len(second)
	if totalReceived != totalWritten {
		t.Errorf("data lost: wrote %d bytes, received %d bytes (lost %d)",
			totalWritten, totalReceived, totalWritten-totalReceived)
	}
}

// --- chunkReader tests ---

func TestChunkReader_NonBlocking(t *testing.T) {
	ctx := context.Background()
	ch := make(chan *bytes.Buffer, 10)
	r := newChunkReader(ctx, ch)

	buf := make([]byte, 100)
	n, err := r.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 bytes from empty channel, got %d", n)
	}
}

// TestChunkReader_CrossChunkBoundary verifies that multiple Read calls
// correctly drain data across chunk boundaries. The reader returns data
// from one internal buffer per Read call (bytes.Buffer.Read returns nil,
// not io.EOF, after a successful read, so the reader's EOF branch is
// never taken and it returns after each buffer read).
func TestChunkReader_CrossChunkBoundary(t *testing.T) {
	ctx := context.Background()
	ch := make(chan *bytes.Buffer, 10)
	r := newChunkReader(ctx, ch)

	ch <- bytes.NewBuffer([]byte("hello"))
	ch <- bytes.NewBuffer([]byte("world"))

	var all []byte
	buf := make([]byte, 10)
	for {
		n, err := r.Read(buf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n == 0 {
			break
		}
		all = append(all, buf[:n]...)
	}

	got := string(all)
	if got != "helloworld" {
		t.Errorf("got %q, want %q", got, "helloworld")
	}
}

func TestChunkReader_DataIntegrity(t *testing.T) {
	ctx := context.Background()
	ch := make(chan *bytes.Buffer, 10)
	r := newChunkReader(ctx, ch)

	patterns := [][]byte{
		{0x01, 0x02, 0x03},
		{0x04, 0x05},
		{0x06, 0x07, 0x08, 0x09},
	}
	expected := []byte{}
	for _, p := range patterns {
		ch <- bytes.NewBuffer(p)
		expected = append(expected, p...)
	}

	var all []byte
	buf := make([]byte, 100)
	for {
		n, err := r.Read(buf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n == 0 {
			break
		}
		all = append(all, buf[:n]...)
	}

	if !bytes.Equal(all, expected) {
		t.Errorf("data mismatch: got %v, want %v", all, expected)
	}
}

// TestChunkReader_PartialFillFromDevice simulates the Duplex playback
// callback: a single Read must fill a device-sized buffer. If the reader
// only returns data from one internal chunk per Read, the device buffer
// is only partially filled even when more chunks are available, causing
// silence gaps in playback.
func TestChunkReader_PartialFillFromDevice(t *testing.T) {
	ctx := context.Background()
	ch := make(chan *bytes.Buffer, 10)
	r := newChunkReader(ctx, ch)

	// Two small chunks available (simulating two audio deltas)
	ch <- bytes.NewBuffer(make([]byte, 200))
	ch <- bytes.NewBuffer(make([]byte, 200))

	// Device wants 960 bytes (20ms at 24kHz mono s16le)
	deviceBuf := make([]byte, 960)
	n, _ := r.Read(deviceBuf)

	// With 400 bytes available, a single Read should ideally return 400,
	// but the reader returns data from one chunk at a time.
	if n == 400 {
		t.Log("reader filled from multiple chunks in one Read (good)")
	} else if n == 200 {
		t.Errorf("reader only returned %d bytes from one chunk; "+
			"400 bytes were available on the channel", n)
	} else {
		t.Errorf("unexpected read size: %d", n)
	}
}

func TestChunkWriterReader_Integration(t *testing.T) {
	ctx := context.Background()
	ch := make(chan *bytes.Buffer, 100)
	w := newChunkWriter(ctx, ch)
	r := newChunkReader(ctx, ch)

	// Write several chunks with time gaps to trigger flushes
	var allWritten []byte
	for i := range 5 {
		data := make([]byte, 100)
		for j := range data {
			data[j] = byte(i)
		}
		w.Write(data)
		allWritten = append(allWritten, data...)
		time.Sleep(260 * time.Millisecond)
	}
	// One final write to trigger flush of the last batch
	w.Write([]byte{0xFF})
	allWritten = append(allWritten, 0xFF)

	// Read everything available
	var allRead []byte
	buf := make([]byte, 1024)
	for {
		n, err := r.Read(buf)
		if err != nil && err != io.EOF {
			t.Fatalf("unexpected error: %v", err)
		}
		if n == 0 {
			break
		}
		allRead = append(allRead, buf[:n]...)
	}

	if len(allRead) == 0 {
		t.Fatal("no data read at all")
	}
	if len(allRead) > len(allWritten) {
		t.Errorf("read more than written: read=%d written=%d", len(allRead), len(allWritten))
	}
}
