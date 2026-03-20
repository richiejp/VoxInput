package audio

import (
	"encoding/binary"
	"log"
	"math"

	"github.com/richiejp/VoxInput/internal/deepvqe"
)

// deepvqeProcessor buffers audio and processes it through DeepVQE in
// ~500ms batches. DeepVQE operates at 16kHz, so resampling is done
// internally when the device rate differs.
type deepvqeProcessor struct {
	engine     *deepvqe.DeepVQE
	deviceRate int // e.g. 24000
	modelRate  int // always 16000
	chunkSize  int // samples at deviceRate for one batch
	micBuf     []int16
	refBuf     []int16

	diagInSum  float64
	diagOutSum float64
	diagRefSum float64
	diagCount  int
}

// NewDeepVQEProcessor creates a batch AEC processor.
// chunkMs is the processing batch size in milliseconds (e.g. 500).
func NewDeepVQEProcessor(engine *deepvqe.DeepVQE, deviceRate, chunkMs int) *deepvqeProcessor {
	modelRate := engine.SampleRate()
	chunkSamples := deviceRate * chunkMs / 1000
	return &deepvqeProcessor{
		engine:     engine,
		deviceRate: deviceRate,
		modelRate:  modelRate,
		chunkSize:  chunkSamples,
	}
}

// Process feeds captured (rec) and playback (play) byte slices (int16 LE PCM
// at deviceRate) into the batch processor. Returns cleaned audio bytes when a
// full batch has accumulated, or nil if still buffering.
func (p *deepvqeProcessor) Process(rec, play []byte) []byte {
	// Convert bytes to int16 and append to buffers
	micSamples := len(rec) / 2
	for i := 0; i < micSamples; i++ {
		p.micBuf = append(p.micBuf, int16(binary.LittleEndian.Uint16(rec[i*2:])))
	}
	refSamples := len(play) / 2
	for i := 0; i < refSamples; i++ {
		p.refBuf = append(p.refBuf, int16(binary.LittleEndian.Uint16(play[i*2:])))
	}

	if len(p.micBuf) < p.chunkSize || len(p.refBuf) < p.chunkSize {
		return nil
	}

	// Take one chunk from each buffer
	micChunk := make([]int16, p.chunkSize)
	copy(micChunk, p.micBuf[:p.chunkSize])
	refChunk := make([]int16, p.chunkSize)
	copy(refChunk, p.refBuf[:p.chunkSize])

	// Resample to model rate if needed
	var mic16k, ref16k []int16
	if p.deviceRate != p.modelRate {
		mic16k = resampleS16Samples(micChunk, p.deviceRate, p.modelRate)
		ref16k = resampleS16Samples(refChunk, p.deviceRate, p.modelRate)
	} else {
		mic16k = micChunk
		ref16k = refChunk
	}

	// Process through DeepVQE
	out16k, err := p.engine.ProcessS16(mic16k, ref16k)
	if err != nil {
		log.Printf("deepvqe: ProcessS16 error: %v", err)
		// Fall through with original mic audio
		out16k = mic16k
	}

	// Resample back to device rate
	var outDevice []int16
	if p.deviceRate != p.modelRate {
		outDevice = resampleS16Samples(out16k, p.modelRate, p.deviceRate)
	} else {
		outDevice = out16k
	}

	// Diagnostics
	p.diagInSum += rmsS16Samples(micChunk)
	p.diagRefSum += rmsS16Samples(refChunk)
	p.diagOutSum += rmsS16Samples(outDevice)
	p.diagCount++
	if p.diagCount%20 == 0 {
		cnt := float64(p.diagCount)
		avgIn := p.diagInSum / cnt
		avgRef := p.diagRefSum / cnt
		avgOut := p.diagOutSum / cnt
		reductionDB := math.NaN()
		if avgIn > 0 {
			reductionDB = 20 * math.Log10(avgOut / avgIn)
		}
		log.Printf("DeepVQE: avgIn=%.0f avgRef=%.0f avgOut=%.0f reduction=%.1fdB chunks=%d",
			avgIn, avgRef, avgOut, reductionDB, p.diagCount)
		p.diagInSum = 0
		p.diagRefSum = 0
		p.diagOutSum = 0
		p.diagCount = 0
	}

	// Advance buffers
	p.micBuf = p.micBuf[p.chunkSize:]
	p.refBuf = p.refBuf[p.chunkSize:]

	// Convert back to bytes
	result := make([]byte, len(outDevice)*2)
	for i, s := range outDevice {
		binary.LittleEndian.PutUint16(result[i*2:], uint16(s))
	}
	return result
}

// resampleS16Samples resamples int16 samples using linear interpolation.
func resampleS16Samples(samples []int16, fromRate, toRate int) []int16 {
	if fromRate == toRate {
		return samples
	}
	ratio := float64(fromRate) / float64(toRate)
	newLen := int(float64(len(samples)) / ratio)
	out := make([]int16, newLen)
	for i := 0; i < newLen; i++ {
		srcPos := float64(i) * ratio
		srcIdx := int(srcPos)
		frac := srcPos - float64(srcIdx)
		if srcIdx >= len(samples)-1 {
			out[i] = samples[len(samples)-1]
			continue
		}
		out[i] = int16(float64(samples[srcIdx])*(1-frac) + float64(samples[srcIdx+1])*frac)
	}
	return out
}

// rmsS16Samples computes RMS of int16 samples.
func rmsS16Samples(samples []int16) float64 {
	if len(samples) == 0 {
		return 0
	}
	sum := 0.0
	for _, s := range samples {
		sum += float64(s) * float64(s)
	}
	return math.Sqrt(sum / float64(len(samples)))
}
