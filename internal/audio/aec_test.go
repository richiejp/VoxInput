package audio

import (
	"encoding/binary"
	"math"
	"math/rand"
	"testing"

	"github.com/richiejp/VoxInput/internal/aec"
)

func generateNoise(rng *rand.Rand, buf []byte) {
	for i := 0; i < len(buf)/2; i++ {
		val := int16(rng.Intn(20000) - 10000)
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(val))
	}
}

func rmspower(buf []byte) float64 {
	n := len(buf) / 2
	if n == 0 {
		return 0
	}
	sum := 0.0
	for i := 0; i < n; i++ {
		s := float64(int16(binary.LittleEndian.Uint16(buf[i*2:])))
		sum += s * s
	}
	return math.Sqrt(sum / float64(n))
}

// TestAECProcessorMismatchedFrames verifies that the processor handles
// callback frame sizes that don't match the AEC frame size.
func TestAECProcessorMismatchedFrames(t *testing.T) {
	sampleRate := 24000
	aecFrameSize := 480 // 20ms
	filterLen := 4800   // 200ms

	c, err := aec.New(aecFrameSize, filterLen, sampleRate)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Destroy()

	proc := newAECProcessor(c, 0)

	// Simulate callback delivering 512 samples (1024 bytes) per call,
	// while AEC expects 480 samples (960 bytes).
	callbackSamples := 512
	callbackBytes := callbackSamples * 2
	aecFrameBytes := aecFrameSize * 2

	rng := rand.New(rand.NewSource(42))

	echoDelayFrames := 3
	echoGain := 0.5

	// Generate all play data upfront so we can create delayed echo
	totalCallbacks := 400
	totalSamples := totalCallbacks * callbackSamples
	allPlay := make([]byte, totalSamples*2)
	generateNoise(rng, allPlay)

	var totalInputSamples, totalOutputSamples int
	convergenceStart := 300

	var convergedEchoRMS, convergedOutRMS float64
	convergedCount := 0

	for cb := 0; cb < totalCallbacks; cb++ {
		playOff := cb * callbackBytes
		play := allPlay[playOff : playOff+callbackBytes]

		// rec = delayed echo
		rec := make([]byte, callbackBytes)
		echoOff := playOff - echoDelayFrames*aecFrameBytes
		if echoOff >= 0 && echoOff+callbackBytes <= len(allPlay) {
			for i := 0; i < callbackSamples; i++ {
				ref := int16(binary.LittleEndian.Uint16(allPlay[echoOff+i*2:]))
				echo := int16(float64(ref) * echoGain)
				binary.LittleEndian.PutUint16(rec[i*2:], uint16(echo))
			}
		}

		totalInputSamples += callbackSamples
		cleaned := proc.Process(rec, play)
		if cleaned != nil {
			totalOutputSamples += len(cleaned) / 2
		}

		if cb >= convergenceStart && cleaned != nil {
			convergedEchoRMS += rmspower(rec)
			convergedOutRMS += rmspower(cleaned)
			convergedCount++
		}
	}

	t.Logf("Total input samples: %d, total output samples: %d",
		totalInputSamples, totalOutputSamples)

	// Verify no samples were lost (within one frame of buffering)
	sampleDiff := totalInputSamples - totalOutputSamples
	if sampleDiff < 0 || sampleDiff > aecFrameSize {
		t.Errorf("sample count mismatch: input=%d output=%d diff=%d (max allowed=%d)",
			totalInputSamples, totalOutputSamples, sampleDiff, aecFrameSize)
	}

	if convergedCount == 0 {
		t.Fatal("no converged frames")
	}

	convergedEchoRMS /= float64(convergedCount)
	convergedOutRMS /= float64(convergedCount)
	reductionDB := 20 * math.Log10(convergedOutRMS / convergedEchoRMS)
	t.Logf("Mismatched frames: echo RMS=%.1f, output RMS=%.1f, reduction=%.1f dB",
		convergedEchoRMS, convergedOutRMS, reductionDB)

	if reductionDB > -6 {
		t.Errorf("expected at least 6 dB echo reduction, got %.1f dB", reductionDB)
	}
}

// TestAECProcessorMatchedFrames verifies that the processor works when
// callback frame size matches the AEC frame size exactly.
func TestAECProcessorMatchedFrames(t *testing.T) {
	sampleRate := 24000
	frameSize := 480
	filterLen := 4800

	c, err := aec.New(frameSize, filterLen, sampleRate)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Destroy()

	proc := newAECProcessor(c, 0)

	callbackBytes := frameSize * 2
	rng := rand.New(rand.NewSource(42))

	echoDelayFrames := 3
	echoGain := 0.5

	totalCallbacks := 300
	totalSamples := totalCallbacks * frameSize
	allPlay := make([]byte, totalSamples*2)
	generateNoise(rng, allPlay)

	convergenceStart := 200
	var convergedEchoRMS, convergedOutRMS float64
	convergedCount := 0

	for cb := 0; cb < totalCallbacks; cb++ {
		playOff := cb * callbackBytes
		play := allPlay[playOff : playOff+callbackBytes]

		rec := make([]byte, callbackBytes)
		echoOff := playOff - echoDelayFrames*callbackBytes
		if echoOff >= 0 {
			for i := 0; i < frameSize; i++ {
				ref := int16(binary.LittleEndian.Uint16(allPlay[echoOff+i*2:]))
				echo := int16(float64(ref) * echoGain)
				binary.LittleEndian.PutUint16(rec[i*2:], uint16(echo))
			}
		}

		cleaned := proc.Process(rec, play)

		if cb >= convergenceStart && cleaned != nil {
			convergedEchoRMS += rmspower(rec)
			convergedOutRMS += rmspower(cleaned)
			convergedCount++
		}
	}

	if convergedCount == 0 {
		t.Fatal("no converged frames")
	}

	convergedEchoRMS /= float64(convergedCount)
	convergedOutRMS /= float64(convergedCount)
	reductionDB := 20 * math.Log10(convergedOutRMS / convergedEchoRMS)
	t.Logf("Matched frames: echo RMS=%.1f, output RMS=%.1f, reduction=%.1f dB",
		convergedEchoRMS, convergedOutRMS, reductionDB)

	if reductionDB > -6 {
		t.Errorf("expected at least 6 dB echo reduction, got %.1f dB", reductionDB)
	}
}
