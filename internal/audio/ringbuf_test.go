package audio

import (
	"sync"
	"testing"
)

func TestInt16Ring_Roundtrip(t *testing.T) {
	r := NewInt16Ring(8)
	in := []int16{1, 2, 3, 4}
	if n := r.Write(in); n != 4 {
		t.Fatalf("Write returned %d, want 4", n)
	}
	if got := r.Len(); got != 4 {
		t.Errorf("Len=%d, want 4", got)
	}

	out := make([]int16, 4)
	if n := r.Read(out); n != 4 {
		t.Fatalf("Read returned %d, want 4", n)
	}
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("out[%d]=%d, want %d", i, out[i], in[i])
		}
	}
	if got := r.Len(); got != 0 {
		t.Errorf("Len after drain=%d, want 0", got)
	}
}

func TestInt16Ring_UnderrunZeroFill(t *testing.T) {
	r := NewInt16Ring(8)
	r.Write([]int16{7, 8})

	out := make([]int16, 5)
	n := r.Read(out)
	if n != 2 {
		t.Errorf("Read returned n=%d, want 2", n)
	}
	want := []int16{7, 8, 0, 0, 0}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("out[%d]=%d, want %d", i, out[i], want[i])
		}
	}
}

func TestInt16Ring_OverrunDropsOldest(t *testing.T) {
	r := NewInt16Ring(4)
	r.Write([]int16{1, 2, 3, 4})
	r.Write([]int16{5, 6})

	out := make([]int16, 4)
	r.Read(out)
	want := []int16{3, 4, 5, 6}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("out[%d]=%d, want %d", i, out[i], want[i])
		}
	}
}

func TestInt16Ring_WriteLargerThanCap(t *testing.T) {
	r := NewInt16Ring(4)
	r.Write([]int16{1, 2, 3, 4, 5, 6, 7, 8})

	if got := r.Len(); got != 4 {
		t.Fatalf("Len=%d, want 4", got)
	}
	out := make([]int16, 4)
	r.Read(out)
	want := []int16{5, 6, 7, 8}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("out[%d]=%d, want %d", i, out[i], want[i])
		}
	}
}

func TestInt16Ring_WrapAround(t *testing.T) {
	r := NewInt16Ring(4)
	r.Write([]int16{1, 2, 3})
	out := make([]int16, 2)
	r.Read(out) // consume 1,2; head=2
	r.Write([]int16{4, 5, 6}) // wraps

	final := make([]int16, 4)
	n := r.Read(final)
	if n != 4 {
		t.Errorf("n=%d, want 4", n)
	}
	want := []int16{3, 4, 5, 6}
	for i := range want {
		if final[i] != want[i] {
			t.Errorf("final[%d]=%d, want %d", i, final[i], want[i])
		}
	}
}

func TestInt16Ring_ConcurrentProducerConsumer(t *testing.T) {
	r := NewInt16Ring(1024)
	var wg sync.WaitGroup
	wg.Add(2)

	const batches = 1000
	const batchSize = 64

	go func() {
		defer wg.Done()
		batch := make([]int16, batchSize)
		for b := 0; b < batches; b++ {
			for i := range batch {
				batch[i] = int16(b)
			}
			r.Write(batch)
		}
	}()

	go func() {
		defer wg.Done()
		out := make([]int16, batchSize)
		for b := 0; b < batches; b++ {
			r.Read(out)
		}
	}()

	wg.Wait()
	// Success = no data race (run with -race), no panic, no deadlock.
}
