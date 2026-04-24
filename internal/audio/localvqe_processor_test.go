package audio

import (
	"bytes"
	"math"
	"testing"
)

type mockEngine struct {
	sampleRate int
	hopLength  int
	callCount  int
}

func (m *mockEngine) ProcessFrameS16(mic, ref []int16) ([]int16, error) {
	m.callCount++
	out := make([]int16, len(mic))
	if m.callCount == 1 {
		return out, nil
	}
	copy(out, mic)
	return out, nil
}

func (m *mockEngine) SampleRate() int { return m.sampleRate }
func (m *mockEngine) HopLength() int  { return m.hopLength }

func newMockProcessor(deviceRate int) (*localvqeProcessor, *mockEngine) {
	e := &mockEngine{sampleRate: 16000, hopLength: 256}
	p := NewLocalVQEProcessor(e, deviceRate)
	return p, e
}

func makeSineS16(n int, freq, rate float64) []int16 {
	out := make([]int16, n)
	for i := range n {
		out[i] = int16(16000 * math.Sin(2*math.Pi*freq*float64(i)/rate))
	}
	return out
}

// --- Processor tests ---

func TestProcess_BufferingReturnsNil(t *testing.T) {
	p, _ := newMockProcessor(16000)
	// 100 samples < 256 hop
	input := s16ToBytes(make([]int16, 100))
	ref := s16ToBytes(make([]int16, 100))
	got := p.Process(input, ref)
	if got != nil {
		t.Errorf("expected nil during buffering, got %d bytes", len(got))
	}
}

func TestProcess_OneHopProducesOutput(t *testing.T) {
	p, _ := newMockProcessor(16000)
	input := s16ToBytes(make([]int16, 256))
	ref := s16ToBytes(make([]int16, 256))
	got := p.Process(input, ref)
	if got == nil {
		t.Fatal("expected output after one full hop, got nil")
	}
	gotSamples := len(got) / 2
	if gotSamples != 256 {
		t.Errorf("expected 256 samples, got %d", gotSamples)
	}
}

func TestProcess_MultipleHopsPerCall(t *testing.T) {
	p, _ := newMockProcessor(16000)
	input := s16ToBytes(make([]int16, 640))
	ref := s16ToBytes(make([]int16, 640))
	got := p.Process(input, ref)
	if got == nil {
		t.Fatal("expected output, got nil")
	}
	gotSamples := len(got) / 2
	// 640 / 256 = 2 full hops, 128 remainder buffered
	if gotSamples != 512 {
		t.Errorf("expected 512 samples (2 hops), got %d", gotSamples)
	}
}

func TestProcess_WithResampling(t *testing.T) {
	p, _ := newMockProcessor(24000)
	// 480 samples at 24kHz -> 320 at 16kHz -> 1 hop (256) with 64 remainder
	input := s16ToBytes(make([]int16, 480))
	ref := s16ToBytes(make([]int16, 480))
	got := p.Process(input, ref)
	if got == nil {
		t.Fatal("expected output after resampled input exceeds one hop, got nil")
	}
	if len(got) == 0 {
		t.Fatal("expected non-empty output")
	}
}

func TestProcess_TotalSampleConservation(t *testing.T) {
	p, _ := newMockProcessor(24000)
	samplesPerCallback := 480
	nCallbacks := 10

	totalInputSamples := 0
	totalOutputSamples := 0
	for range nCallbacks {
		input := s16ToBytes(make([]int16, samplesPerCallback))
		ref := s16ToBytes(make([]int16, samplesPerCallback))
		totalInputSamples += samplesPerCallback

		got := p.Process(input, ref)
		if got != nil {
			totalOutputSamples += len(got) / 2
		}
	}

	// Resampling 24->16->24 should approximately conserve sample count.
	// Allow tolerance for rounding and initial buffering delay.
	diff := totalInputSamples - totalOutputSamples
	maxDrift := samplesPerCallback // at most one callback's worth of buffered remainder
	if diff < 0 || diff > maxDrift {
		t.Errorf("sample count drift too large: input=%d output=%d diff=%d (max allowed %d)",
			totalInputSamples, totalOutputSamples, diff, maxDrift)
	}
}

func TestProcess_NonZeroInputProducesNonZeroOutput(t *testing.T) {
	p, e := newMockProcessor(16000)

	// First hop: warmup, engine returns zeros
	sine := makeSineS16(256, 1000, 16000)
	ref := make([]int16, 256)
	got := p.Process(s16ToBytes(sine), s16ToBytes(ref))
	if got == nil {
		t.Fatal("expected output on first hop")
	}
	if e.callCount != 1 {
		t.Fatalf("expected 1 engine call, got %d", e.callCount)
	}

	// Second hop: engine returns mic passthrough
	got = p.Process(s16ToBytes(sine), s16ToBytes(ref))
	if got == nil {
		t.Fatal("expected output on second hop")
	}
	outSamples := bytesToS16(got)
	rms := rmsS16Samples(outSamples)
	if rms == 0 {
		t.Error("expected non-zero RMS output after warmup")
	}
}

// --- Helper function tests ---

func TestBytesToS16_RoundTrip(t *testing.T) {
	original := []byte{0x01, 0x02, 0x03, 0x04, 0xFF, 0x7F, 0x00, 0x80}
	samples := bytesToS16(original)
	result := s16ToBytes(samples)
	if !bytes.Equal(original, result) {
		t.Errorf("round-trip failed: got %v, want %v", result, original)
	}
}

func TestRmsS16Samples_Zero(t *testing.T) {
	samples := make([]int16, 100)
	if rmsS16Samples(samples) != 0 {
		t.Error("expected 0 RMS for silence")
	}
}

func TestRmsS16Samples_NonZero(t *testing.T) {
	samples := []int16{100, -100, 100, -100}
	rms := rmsS16Samples(samples)
	if rms != 100 {
		t.Errorf("expected RMS=100, got %.2f", rms)
	}
}

func TestRmsS16Samples_Empty(t *testing.T) {
	if rmsS16Samples(nil) != 0 {
		t.Error("expected 0 RMS for nil slice")
	}
}

// --- Resampling tests ---

func TestResampleS16Samples_Identity(t *testing.T) {
	input := makeSineS16(480, 1000, 24000)
	got := resampleS16Samples(input, 24000, 24000)
	if len(got) != len(input) {
		t.Errorf("identity resample changed length: got %d, want %d", len(got), len(input))
	}
}

func TestResampleS16Samples_EmptyInput(t *testing.T) {
	got := resampleS16Samples(nil, 24000, 16000)
	if len(got) != 0 {
		t.Errorf("expected empty output, got %d samples", len(got))
	}
}

func TestResampleS16Samples_RoundTrip(t *testing.T) {
	input := makeSineS16(480, 1000, 24000)
	down := resampleS16Samples(input, 24000, 16000)
	up := resampleS16Samples(down, 16000, 24000)
	diff := len(input) - len(up)
	if diff < -1 || diff > 1 {
		t.Errorf("round-trip sample count drift: input=%d output=%d diff=%d",
			len(input), len(up), diff)
	}
}

func TestResampleS16Samples_KnownRatio(t *testing.T) {
	input := makeSineS16(480, 1000, 24000)

	down := resampleS16Samples(input, 24000, 16000)
	if len(down) != 320 {
		t.Errorf("24000->16000: expected 320 samples, got %d", len(down))
	}

	up := resampleS16Samples(down, 16000, 24000)
	if len(up) != 480 {
		t.Errorf("16000->24000: expected 480 samples, got %d", len(up))
	}
}
