package aec

import (
	"encoding/binary"
	"math"
	"math/rand"
	"testing"
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

func TestCancellerReducesEcho(t *testing.T) {
	sampleRate := 24000
	frameSize := 480
	filterLen := 4800

	c, err := New(frameSize, filterLen, sampleRate)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Destroy()

	frameBytes := frameSize * 2
	play := make([]byte, frameBytes)
	rec := make([]byte, frameBytes)
	out := make([]byte, frameBytes)

	rng := rand.New(rand.NewSource(42))

	// Echo parameters: 3 frames (60ms) delay, 0.5 attenuation
	echoDelayFrames := 3
	echoGain := 0.5

	// Store play history for generating delayed echo
	playHistory := make([][]byte, 0)

	totalFrames := 300
	convergenceStart := 200

	var convergedEchoRMS, convergedOutRMS float64
	convergedCount := 0

	for frame := 0; frame < totalFrames; frame++ {
		generateNoise(rng, play)

		saved := make([]byte, frameBytes)
		copy(saved, play)
		playHistory = append(playHistory, saved)

		// rec = delayed echo of play
		echoFrame := frame - echoDelayFrames
		if echoFrame >= 0 {
			for i := 0; i < frameSize; i++ {
				ref := int16(binary.LittleEndian.Uint16(playHistory[echoFrame][i*2:]))
				echo := int16(float64(ref) * echoGain)
				binary.LittleEndian.PutUint16(rec[i*2:], uint16(echo))
			}
		} else {
			for i := range rec {
				rec[i] = 0
			}
		}

		c.Process(rec, play, out)

		if frame >= convergenceStart {
			convergedEchoRMS += rmspower(rec)
			convergedOutRMS += rmspower(out)
			convergedCount++
		}
	}

	convergedEchoRMS /= float64(convergedCount)
	convergedOutRMS /= float64(convergedCount)

	if convergedEchoRMS == 0 {
		t.Fatal("echo RMS is zero, test is broken")
	}

	reductionDB := 20 * math.Log10(convergedOutRMS/convergedEchoRMS)
	t.Logf("After convergence: echo RMS=%.1f, output RMS=%.1f, reduction=%.1f dB",
		convergedEchoRMS, convergedOutRMS, reductionDB)

	if reductionDB > -6 {
		t.Errorf("expected at least 6 dB echo reduction, got %.1f dB", reductionDB)
	}
}
