package audio

import (
	"encoding/binary"
	"log"
	"math"
)

type LocalVQEEngine interface {
	ProcessFrameS16(mic, ref []int16) ([]int16, error)
	SampleRate() int
	HopLength() int
}

// localvqeProcessor streams audio through LocalVQE one hop at a time.
// LocalVQE operates at 16kHz, so resampling is done internally when
// the device rate differs.
type localvqeProcessor struct {
	engine     LocalVQEEngine
	deviceRate int
	modelRate  int
	hopLength  int
	micBuf     []int16
	refBuf     []int16

	diagInSum  float64
	diagOutSum float64
	diagRefSum float64
	diagCount  int
}

// NewLocalVQEProcessor creates a streaming AEC processor.
func NewLocalVQEProcessor(engine LocalVQEEngine, deviceRate int) *localvqeProcessor {
	return &localvqeProcessor{
		engine:     engine,
		deviceRate: deviceRate,
		modelRate:  engine.SampleRate(),
		hopLength:  engine.HopLength(),
	}
}

// Process feeds captured (rec) and playback (play) byte slices (int16 LE PCM
// at deviceRate) into the streaming processor. Returns cleaned audio bytes,
// or nil if less than one hop is available (rare, only during warmup).
func (p *localvqeProcessor) Process(rec, play []byte) []byte {
	micSamples := bytesToS16(rec)
	refSamples := bytesToS16(play)

	if p.deviceRate != p.modelRate {
		micSamples = resampleS16Samples(micSamples, p.deviceRate, p.modelRate)
		refSamples = resampleS16Samples(refSamples, p.deviceRate, p.modelRate)
	}

	p.micBuf = append(p.micBuf, micSamples...)
	p.refBuf = append(p.refBuf, refSamples...)

	var outSamples []int16
	hop := p.hopLength

	for len(p.micBuf) >= hop && len(p.refBuf) >= hop {
		out, err := p.engine.ProcessFrameS16(p.micBuf[:hop], p.refBuf[:hop])
		if err != nil {
			log.Printf("localvqe: ProcessFrameS16 error: %v", err)
			out = make([]int16, hop)
			copy(out, p.micBuf[:hop])
		}

		p.diagInSum += rmsS16Samples(p.micBuf[:hop])
		p.diagRefSum += rmsS16Samples(p.refBuf[:hop])
		p.diagOutSum += rmsS16Samples(out)
		p.diagCount++

		outSamples = append(outSamples, out...)
		p.micBuf = p.micBuf[hop:]
		p.refBuf = p.refBuf[hop:]
	}

	if len(outSamples) == 0 {
		return nil
	}

	if p.diagCount > 0 && p.diagCount%500 == 0 {
		cnt := float64(p.diagCount)
		avgIn := p.diagInSum / cnt
		avgRef := p.diagRefSum / cnt
		avgOut := p.diagOutSum / cnt
		reductionDB := math.NaN()
		if avgIn > 0 {
			reductionDB = 20 * math.Log10(avgOut/avgIn)
		}
		log.Printf("LocalVQE: avgIn=%.0f avgRef=%.0f avgOut=%.0f reduction=%.1fdB hops=%d",
			avgIn, avgRef, avgOut, reductionDB, p.diagCount)
		p.diagInSum = 0
		p.diagRefSum = 0
		p.diagOutSum = 0
		p.diagCount = 0
	}

	if p.deviceRate != p.modelRate {
		outSamples = resampleS16Samples(outSamples, p.modelRate, p.deviceRate)
	}

	return s16ToBytes(outSamples)
}

func bytesToS16(b []byte) []int16 {
	n := len(b) / 2
	out := make([]int16, n)
	for i := range n {
		out[i] = int16(binary.LittleEndian.Uint16(b[i*2:]))
	}
	return out
}

func s16ToBytes(samples []int16) []byte {
	out := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(s))
	}
	return out
}

// resampleS16Samples resamples int16 samples using linear interpolation.
func resampleS16Samples(samples []int16, fromRate, toRate int) []int16 {
	if fromRate == toRate {
		return samples
	}
	ratio := float64(fromRate) / float64(toRate)
	newLen := int(float64(len(samples)) / ratio)
	out := make([]int16, newLen)
	for i := range newLen {
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
