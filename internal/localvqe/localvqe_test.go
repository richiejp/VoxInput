package localvqe

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

// Integration tests exercising the real shared library and models through
// the same purego path the app uses. They default to the CMake build
// outputs and skip when those are absent, so a plain `go test ./...`
// stays green without a native build.

func testAsset(t *testing.T, env, rel string) string {
	t.Helper()
	if p := os.Getenv(env); p != "" {
		return p
	}
	p := filepath.Join("..", "..", "build", rel)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("%s not set and %s not present (run the CMake build first)", env, p)
	}
	return p
}

func rmsS16(s []int16) float64 {
	var sum float64
	for _, v := range s {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum / float64(len(s)))
}

// echoSignal is a deterministic speech-band reference: a sum of sines the
// adaptive echo-cancel front-end must track.
func echoSignal(n, offset int, rate float64) []int16 {
	out := make([]int16, n)
	for i := range n {
		t := float64(offset+i) / rate
		v := 6000*math.Sin(2*math.Pi*300*t) +
			4000*math.Sin(2*math.Pi*700*t) +
			2000*math.Sin(2*math.Pi*1200*t)
		out[i] = int16(v)
	}
	return out
}

// TestEchoSuppression streams a pure-echo scenario (mic is an attenuated
// copy of the reference, no near-end speech) through each bundled model
// and requires the output to be strongly attenuated relative to the mic.
func TestEchoSuppression(t *testing.T) {
	lib := testAsset(t, "VOXINPUT_LOCALVQE_LIB", filepath.Join("bin", "liblocalvqe.so"))

	for _, variant := range SupportedModelVariants {
		t.Run(string(variant), func(t *testing.T) {
			model := testAsset(t, "VOXINPUT_LOCALVQE_MODEL",
				filepath.Join("share", "voxinput", modelFileName(variant)))

			engine, err := New(lib, model)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(engine.Close)

			if got := engine.SampleRate(); got != 16000 {
				t.Fatalf("SampleRate: got %d, want 16000", got)
			}
			hop := engine.HopLength()
			if hop <= 0 {
				t.Fatalf("HopLength: got %d", hop)
			}

			const (
				totalHops   = 100
				settleHops  = 50
				maxEchoGain = 0.25 // >= ~12 dB suppression once settled
			)
			var micRMS, outRMS float64
			out := make([]int16, hop)
			for h := range totalHops {
				ref := echoSignal(hop, h*hop, float64(engine.SampleRate()))
				mic := make([]int16, hop)
				for i, v := range ref {
					mic[i] = v / 2
				}

				if err := engine.ProcessFrameS16Into(mic, ref, out); err != nil {
					t.Fatalf("ProcessFrameS16Into hop %d: %v", h, err)
				}
				if h >= settleHops {
					micRMS += rmsS16(mic)
					outRMS += rmsS16(out)
				}
			}

			if outRMS > micRMS*maxEchoGain {
				t.Errorf("echo not suppressed: settled output RMS %.1f vs mic RMS %.1f (gain %.3f, want <= %.2f)",
					outRMS/float64(totalHops-settleHops),
					micRMS/float64(totalHops-settleHops),
					outRMS/micRMS, maxEchoGain)
			}

			engine.Reset()
		})
	}
}
