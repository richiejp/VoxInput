package audio

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/richiejp/VoxInput/internal/aec"
)

// TestAECDumpShiftAnalysis loads dumped mic and speaker audio files and
// runs echo cancellation at various time shifts between them. This helps
// determine whether a timing offset between the two streams explains
// poor AEC performance.
//
// Set VOXINPUT_DUMP_DIR to the directory containing mic.raw, spk.raw
// and meta.json (produced by --dump-audio).
//
// Example:
//
//	VOXINPUT_DUMP_DIR=/tmp/aec-dump go test ./internal/audio/ -run TestAECDumpShiftAnalysis -v
func TestAECDumpShiftAnalysis(t *testing.T) {
	dumpDir := os.Getenv("VOXINPUT_DUMP_DIR")
	if dumpDir == "" {
		t.Skip("VOXINPUT_DUMP_DIR not set")
	}

	metaPath := filepath.Join(dumpDir, "meta.json")
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	var meta struct {
		SampleRate int `json:"sampleRate"`
	}
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("parse meta.json: %v", err)
	}
	if meta.SampleRate == 0 {
		t.Fatal("sampleRate is 0 in meta.json")
	}

	micData, err := os.ReadFile(filepath.Join(dumpDir, "mic.raw"))
	if err != nil {
		t.Fatalf("read mic.raw: %v", err)
	}
	spkData, err := os.ReadFile(filepath.Join(dumpDir, "spk.raw"))
	if err != nil {
		t.Fatalf("read spk.raw: %v", err)
	}

	t.Logf("Loaded mic=%d bytes (%d samples), spk=%d bytes (%d samples), rate=%d Hz",
		len(micData), len(micData)/2, len(spkData), len(spkData)/2, meta.SampleRate)

	micRMS := rmspower(micData)
	spkRMS := rmspower(spkData)
	t.Logf("Overall mic RMS=%.1f, spk RMS=%.1f", micRMS, spkRMS)

	if spkRMS < 1 {
		t.Fatal("speaker data is silent, nothing to cancel")
	}

	sampleRate := meta.SampleRate
	periodMs := 20
	frameSize := sampleRate * periodMs / 1000
	filterLen := sampleRate * 200 / 1000

	// Try shifts from -500ms to +500ms in 10ms steps.
	// Positive shift means the speaker data is delayed relative to the mic
	// (i.e. the echo appears in the mic before the reference signal).
	stepMs := 10
	minShiftMs := -500
	maxShiftMs := 500

	type result struct {
		shiftMs     int
		reductionDB float64
		avgIn       float64
		avgOut      float64
	}

	var results []result
	bestReduction := math.Inf(1)
	bestShiftMs := 0

	for shiftMs := minShiftMs; shiftMs <= maxShiftMs; shiftMs += stepMs {
		shiftSamples := shiftMs * sampleRate / 1000
		shiftBytes := shiftSamples * 2

		// Apply shift: positive shift means we advance spk relative to mic.
		var mic, spk []byte
		if shiftBytes >= 0 {
			// Shift speaker forward: skip first shiftBytes of spk
			if shiftBytes >= len(spkData) {
				continue
			}
			spk = spkData[shiftBytes:]
			mic = micData
		} else {
			// Shift speaker backward: skip first -shiftBytes of mic
			off := -shiftBytes
			if off >= len(micData) {
				continue
			}
			mic = micData[off:]
			spk = spkData
		}

		// Truncate to equal length
		minLen := len(mic)
		if len(spk) < minLen {
			minLen = len(spk)
		}
		// Align to frame boundary
		frameBytes := frameSize * 2
		minLen = (minLen / frameBytes) * frameBytes
		if minLen < frameBytes*10 {
			continue
		}
		mic = mic[:minLen]
		spk = spk[:minLen]

		c, err := aec.New(frameSize, filterLen, sampleRate)
		if err != nil {
			t.Fatalf("aec.New: %v", err)
		}

		out := make([]byte, frameBytes)
		totalFrames := minLen / frameBytes

		// Use second half for measurement (after convergence)
		convergenceFrames := totalFrames / 2
		var sumIn, sumOut float64
		measured := 0

		for f := 0; f < totalFrames; f++ {
			off := f * frameBytes
			c.Process(mic[off:off+frameBytes], spk[off:off+frameBytes], out)

			if f >= convergenceFrames {
				sumIn += rmspower(mic[off : off+frameBytes])
				sumOut += rmspower(out)
				measured++
			}
		}

		c.Destroy()

		if measured == 0 {
			continue
		}

		avgIn := sumIn / float64(measured)
		avgOut := sumOut / float64(measured)
		reductionDB := math.NaN()
		if avgIn > 0 {
			reductionDB = 20 * math.Log10(avgOut/avgIn)
		}

		results = append(results, result{
			shiftMs:     shiftMs,
			reductionDB: reductionDB,
			avgIn:       avgIn,
			avgOut:      avgOut,
		})

		if reductionDB < bestReduction {
			bestReduction = reductionDB
			bestShiftMs = shiftMs
		}
	}

	t.Logf("\n%-10s %-12s %-10s %-10s", "Shift(ms)", "Reduction", "AvgIn", "AvgOut")
	t.Logf("%-10s %-12s %-10s %-10s", "--------", "---------", "-----", "------")
	for _, r := range results {
		marker := ""
		if r.shiftMs == bestShiftMs {
			marker = " <-- BEST"
		}
		t.Logf("%-10d %-12.1f %-10.0f %-10.0f%s",
			r.shiftMs, r.reductionDB, r.avgIn, r.avgOut, marker)
	}

	t.Logf("\nBest shift: %d ms (%.1f dB reduction)", bestShiftMs, bestReduction)

	// Also compute cross-correlation to find the raw delay
	crossCorrelationAnalysis(t, micData, spkData, sampleRate)
}

// crossCorrelationAnalysis computes the cross-correlation between mic and spk
// signals to estimate the acoustic delay directly, without running AEC.
func crossCorrelationAnalysis(t *testing.T, mic, spk []byte, sampleRate int) {
	t.Helper()

	maxLagMs := 500
	maxLagSamples := maxLagMs * sampleRate / 1000

	micSamples := len(mic) / 2
	spkSamples := len(spk) / 2
	n := micSamples
	if spkSamples < n {
		n = spkSamples
	}

	// Downsample for speed: take every 4th sample
	step := 4
	micF := make([]float64, n/step)
	spkF := make([]float64, n/step)
	for i := range micF {
		micF[i] = float64(int16(binary.LittleEndian.Uint16(mic[i*step*2:])))
		spkF[i] = float64(int16(binary.LittleEndian.Uint16(spk[i*step*2:])))
	}

	maxLagDown := maxLagSamples / step
	bestLag := 0
	bestCorr := math.Inf(-1)

	overlapN := len(micF) - maxLagDown
	if overlapN < 100 {
		t.Log("crossCorrelation: not enough data")
		return
	}

	for lag := -maxLagDown; lag <= maxLagDown; lag++ {
		sum := 0.0
		count := 0
		for i := 0; i < overlapN; i++ {
			j := i + lag
			if j < 0 || j >= len(spkF) {
				continue
			}
			sum += micF[i] * spkF[j]
			count++
		}
		if count > 0 {
			corr := sum / float64(count)
			if corr > bestCorr {
				bestCorr = corr
				bestLag = lag
			}
		}
	}

	lagMs := float64(bestLag*step) * 1000.0 / float64(sampleRate)
	t.Logf("\nCross-correlation peak at lag=%d samples (%+.1f ms)",
		bestLag*step, lagMs)
	t.Logf("Positive lag = speaker leads mic (echo arrives after playback)")
}
