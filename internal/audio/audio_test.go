package audio

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func makeS16Bytes(nSamples int) []byte {
	b := make([]byte, nSamples*2)
	for i := range nSamples {
		binary.LittleEndian.PutUint16(b[i*2:], uint16(i%256))
	}
	return b
}

// --- resampleS16 (byte-level) tests ---

func TestResampleS16_Identity(t *testing.T) {
	input := makeS16Bytes(480)
	got := resampleS16(input, 24000, 24000)
	if !bytes.Equal(got, input) {
		t.Errorf("identity resample changed data: got %d bytes, want %d", len(got), len(input))
	}
}

func TestResampleS16_RoundTrip(t *testing.T) {
	input := makeS16Bytes(480)
	down := resampleS16(input, 24000, 16000)
	up := resampleS16(down, 16000, 24000)
	inputSamples := len(input) / 2
	outputSamples := len(up) / 2
	diff := inputSamples - outputSamples
	if diff < -1 || diff > 1 {
		t.Errorf("round-trip sample count drift: input=%d output=%d",
			inputSamples, outputSamples)
	}
}

// --- Duplex callback double-send test ---

// TestDuplexCallback_NoDoubleSend simulates the Duplex callback pattern
// from audio.go to verify that when the processor is buffering (returns
// nil), raw input is NOT written — preventing double-send of samples that
// the processor will emit later.
//
// Uses a small callback size (100 samples at 16kHz, < 256 hop) so the
// processor needs multiple callbacks to accumulate one hop.
func TestDuplexCallback_NoDoubleSend(t *testing.T) {
	p, _ := newMockProcessor(16000)
	callbackSamples := 100 // < 256 hop, forces buffering
	nCallbacks := 20

	var writer bytes.Buffer
	totalInputBytes := 0

	for range nCallbacks {
		inputSamples := s16ToBytes(make([]int16, callbackSamples))
		outputSamples := s16ToBytes(make([]int16, callbackSamples))
		totalInputBytes += len(inputSamples)

		// Replicate Duplex callback logic (audio.go:297-315)
		samplesToWrite := inputSamples

		cleaned := p.Process(inputSamples, outputSamples)
		if cleaned != nil {
			samplesToWrite = cleaned
		} else {
			samplesToWrite = nil
		}

		if len(samplesToWrite) > 0 {
			writer.Write(samplesToWrite)
		}
	}

	if writer.Len() > totalInputBytes {
		t.Errorf("double-send: wrote %d bytes but input was %d bytes (extra: %d samples)",
			writer.Len(), totalInputBytes,
			(writer.Len()-totalInputBytes)/2)
	}
}
