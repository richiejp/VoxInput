package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"math"
	"strings"

	"github.com/gen2brain/malgo"
	"github.com/richiejp/VoxInput/internal/aec"
)

// Copied from malgo/examples/io_api

// StreamConfig describes the parameters for an audio stream.
// Default values will pick the defaults of the default device.
type StreamConfig struct {
	Format           malgo.FormatType
	Channels         int
	SampleRate       int
	InputSampleRate  int
	OutputSampleRate int
	PeriodMs         int
	MalgoContext     malgo.Context
	CaptureDeviceID  *malgo.DeviceID
}

func (config StreamConfig) asDeviceConfig(deviceType malgo.DeviceType) malgo.DeviceConfig {
	deviceConfig := malgo.DefaultDeviceConfig(deviceType)
	if config.Format != malgo.FormatUnknown {
		deviceConfig.Capture.Format = config.Format
		deviceConfig.Playback.Format = config.Format
	}
	if config.Channels != 0 {
		deviceConfig.Capture.Channels = uint32(config.Channels)
		deviceConfig.Playback.Channels = uint32(config.Channels)
	}
	if config.SampleRate != 0 {
		deviceConfig.SampleRate = uint32(config.SampleRate)
	}
	if config.CaptureDeviceID != nil && (deviceType == malgo.Capture || deviceType == malgo.Duplex) {
		deviceConfig.Capture.DeviceID = config.CaptureDeviceID.Pointer()
	}
	if config.PeriodMs != 0 {
		deviceConfig.PeriodSizeInMilliseconds = uint32(config.PeriodMs)
	}
	return deviceConfig
}

// SetCaptureDeviceByName sets the capture DeviceID by matching device name.
// Returns true if found and set.
func (c *StreamConfig) SetCaptureDeviceByName(mctx *malgo.Context, name string) (bool, error) {
	if name == "" {
		return false, nil
	}
	devices, err := mctx.Devices(malgo.Capture)
	if err != nil {
		return false, err
	}
	for _, d := range devices {
		if strings.TrimSpace(d.Name()) == name {
			id := malgo.DeviceID(d.ID)
			c.CaptureDeviceID = &id
			return true, nil
		}
	}
	return false, nil
}

func stream(ctx context.Context, abortChan chan error, config StreamConfig, deviceType malgo.DeviceType, deviceCallbacks malgo.DeviceCallbacks) error {
	deviceConfig := config.asDeviceConfig(deviceType)
	device, err := malgo.InitDevice(config.MalgoContext, deviceConfig, deviceCallbacks)
	if err != nil {
		return err
	}
	defer device.Uninit()

	err = device.Start()
	if err != nil {
		return err
	}

	ctxChan := ctx.Done()
	if ctxChan != nil {
		select {
		case <-ctxChan:
			err = ctx.Err()
		case err = <-abortChan:
		}
	} else {
		err = <-abortChan
	}

	return err
}

// ListCaptureDevices returns all available capture devices with their names and IDs.
func ListCaptureDevices() error {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return err
	}
	defer ctx.Uninit()
	mctx := &ctx.Context

	devs, err := mctx.Devices(malgo.Capture)
	if err != nil {
		return err
	}

	fmt.Println("Available capture devices:")
	for i, d := range devs {
		note := ""
		if d.IsDefault != 0 {
			note = "(DEFAULT)"
		}
		fmt.Printf("  %d: \"%s\" %s\n  ID: %s\n", i, d.Name(), note, d.ID.String())
	}
	if len(devs) == 0 {
		fmt.Println("  (none found)")
	}

	return nil
}

// Capture records incoming samples into the provided writer.
// The function initializes a capture device in the default context using the
// provided stream configuration.
// XXX: Capture, Duplex and Playback are mutually exclusive, only use one at a time
func Capture(ctx context.Context, w io.Writer, config StreamConfig) error {
	abortChan := make(chan error)
	defer close(abortChan)
	aborted := false

	deviceCallbacks := malgo.DeviceCallbacks{
		Data: func(outputSamples, inputSamples []byte, frameCount uint32) {
			if aborted {
				return
			}

			if len(inputSamples) > 0 {
				_, err := w.Write(inputSamples)
				if err != nil {
					aborted = true
					abortChan <- err
				}
			}
		},
	}

	return stream(ctx, abortChan, config, malgo.Capture, deviceCallbacks)
}

// resampleS16 resamples 16-bit PCM audio from fromRate to toRate using linear interpolation.
func rmsS16(buf []byte) float64 {
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

// samples is the input audio data as bytes (int16 little-endian).
// Returns the resampled audio as bytes.
func resampleS16(samples []byte, fromRate, toRate int) []byte {
	if fromRate == toRate {
		return samples
	}

	numSamples := len(samples) / 2
	ratio := float64(fromRate) / float64(toRate)
	newNumSamples := int(float64(numSamples) / ratio)

	resampled := make([]byte, newNumSamples*2)

	for i := 0; i < newNumSamples; i++ {
		srcPos := float64(i) * ratio
		srcIdx := int(srcPos)
		frac := srcPos - float64(srcIdx)

		if srcIdx >= numSamples-1 {
			binary.LittleEndian.PutUint16(resampled[i*2:], binary.LittleEndian.Uint16(samples[(numSamples-1)*2:]))
			continue
		}

		sample1 := int16(binary.LittleEndian.Uint16(samples[srcIdx*2:]))
		sample2 := int16(binary.LittleEndian.Uint16(samples[(srcIdx+1)*2:]))

		interpolated := int16(float64(sample1)*(1-frac) + float64(sample2)*frac)
		binary.LittleEndian.PutUint16(resampled[i*2:], uint16(interpolated))
	}

	return resampled
}

// aecProcessor buffers audio to process AEC in fixed-size frames.
// Audio backends may deliver frames of varying sizes, but SpeexDSP
// requires exact frame sizes as configured during initialization.
type aecProcessor struct {
	canceller  *aec.Canceller
	frameBytes int
	recBuf     []byte
	playBuf    []byte
	outFrame   []byte

	diagInSum  float64
	diagRefSum float64
	diagOutSum float64
	diagRefMax float64
	diagCount  int
}

// newAECProcessor creates a new AEC frame processor. delayBytes prepends
// silence to the playback reference buffer so that mic sample at time t
// is paired with the speaker sample from t-delay. This compensates for
// the acoustic path delay and lets the filter converge much faster.
func newAECProcessor(c *aec.Canceller, delayBytes int) *aecProcessor {
	fb := c.FrameSize() * 2
	p := &aecProcessor{
		canceller:  c,
		frameBytes: fb,
		outFrame:   make([]byte, fb),
	}
	if delayBytes > 0 {
		p.playBuf = make([]byte, delayBytes)
	}
	return p
}

// Process feeds captured (rec) and playback (play) samples into the echo
// canceller. It returns the cleaned signal, which may be shorter or longer
// than rec depending on buffering. Returns nil if not enough data has
// accumulated for a complete frame yet.
func (p *aecProcessor) Process(rec, play []byte) []byte {
	p.recBuf = append(p.recBuf, rec...)
	p.playBuf = append(p.playBuf, play...)

	var cleaned []byte
	recOff := 0
	playOff := 0
	for recOff+p.frameBytes <= len(p.recBuf) && playOff+p.frameBytes <= len(p.playBuf) {
		p.canceller.Process(p.recBuf[recOff:recOff+p.frameBytes], p.playBuf[playOff:playOff+p.frameBytes], p.outFrame)
		cleaned = append(cleaned, p.outFrame...)
		recOff += p.frameBytes
		playOff += p.frameBytes
	}

	if recOff > 0 {
		n := copy(p.recBuf, p.recBuf[recOff:])
		p.recBuf = p.recBuf[:n]
	}
	if playOff > 0 {
		n := copy(p.playBuf, p.playBuf[playOff:])
		p.playBuf = p.playBuf[:n]
	}

	return cleaned
}

// DuplexOpts holds optional parameters for Duplex.
type DuplexOpts struct {
	// Raw mic samples at the hardware sample rate, before AEC.
	DumpInput io.Writer
	// Raw speaker samples at the hardware sample rate, after resampling.
	DumpOutput io.Writer
	// Cleaned mic samples at the hardware sample rate, after AEC processing.
	DumpProcessed io.Writer
	// ReferenceDelayMs delays the speaker reference by this many ms to
	// compensate for the acoustic path delay between speaker and mic.
	ReferenceDelayMs int
}

// Duplex streams audio from a reader to the playback device and captures audio
// from the capture device to a writer.
// It initializes a duplex device in the default context using the provided stream configuration.
// It expects both r and w to be non-nil.
// If InputSampleRate and OutputSampleRate differ from SampleRate, resampling is performed.
// If echoCanceller is non-nil, the speaker output is subtracted from the mic input.
// XXX: Capture, Duplex and Playback are mutually exclusive, only use one at a time
func Duplex(ctx context.Context, r io.Reader, w io.Writer, config StreamConfig, echoCanceller *aec.Canceller, opts *DuplexOpts) error {
	abortChan := make(chan error)
	defer close(abortChan)
	aborted := false

	needInputResample := config.InputSampleRate != 0 && config.InputSampleRate != config.SampleRate
	needOutputResample := config.OutputSampleRate != 0 && config.OutputSampleRate != config.SampleRate

	var aecProc *aecProcessor
	if echoCanceller != nil {
		delayBytes := 0
		if opts != nil && opts.ReferenceDelayMs > 0 {
			delaySamples := config.SampleRate * opts.ReferenceDelayMs / 1000
			delayBytes = delaySamples * 2
		}
		aecProc = newAECProcessor(echoCanceller, delayBytes)
	}

	callbackCount := 0
	deviceCallbacks := malgo.DeviceCallbacks{
		Data: func(outputSamples, inputSamples []byte, frameCount uint32) {
			if aborted {
				return
			}
			callbackCount++

			if aecProc != nil && callbackCount == 1 {
				log.Printf("AEC: first callback: outputSamples=%d inputSamples=%d frameCount=%d aecFrame=%d",
					len(outputSamples), len(inputSamples), frameCount, aecProc.frameBytes)
			}

			// Process output (speaker) first so the reference signal is
			// available for echo cancellation before we handle input.
			if len(outputSamples) > 0 {
				if frameCount == 0 {
					return
				}

				// Calculate how many bytes we need to read from the reader
				// based on the resampling ratio
				bytesToRead := len(outputSamples)
				if needOutputResample {
					ratio := float64(config.OutputSampleRate) / float64(config.SampleRate)
					bytesToRead = int(float64(len(outputSamples)) * ratio)
				}

				readBuf := make([]byte, bytesToRead)
				read, err := r.Read(readBuf)
				if err != nil {
					if err == io.EOF {
						for i := 0; i < len(outputSamples); i++ {
							outputSamples[i] = 0
						}
						aborted = true
						abortChan <- io.EOF
						return
					}
					aborted = true
					abortChan <- err
					return
				}

				if needOutputResample {
					resampled := resampleS16(readBuf[:read], config.OutputSampleRate, config.SampleRate)
					copy(outputSamples, resampled)
					for i := len(resampled); i < len(outputSamples); i++ {
						outputSamples[i] = 0
					}
				} else {
					copy(outputSamples, readBuf[:read])
					for i := read; i < len(outputSamples); i++ {
						outputSamples[i] = 0
					}
				}
			}

			if opts != nil && opts.DumpOutput != nil && len(outputSamples) > 0 {
				opts.DumpOutput.Write(outputSamples)
			}

			if len(inputSamples) > 0 {
				if opts != nil && opts.DumpInput != nil {
					opts.DumpInput.Write(inputSamples)
				}

				samplesToWrite := inputSamples

				if aecProc != nil && len(outputSamples) > 0 {
					cleaned := aecProc.Process(inputSamples, outputSamples)
					if len(cleaned) > 0 {
						samplesToWrite = cleaned
						if opts != nil && opts.DumpProcessed != nil {
							opts.DumpProcessed.Write(cleaned)
						}
					} else {
						return
					}

					refRMS := rmsS16(outputSamples)
					aecProc.diagInSum += rmsS16(inputSamples)
					aecProc.diagRefSum += refRMS
					aecProc.diagOutSum += rmsS16(cleaned)
					if refRMS > aecProc.diagRefMax {
						aecProc.diagRefMax = refRMS
					}
					aecProc.diagCount++

					if callbackCount%500 == 0 && aecProc.diagCount > 0 {
						cnt := float64(aecProc.diagCount)
						avgIn := aecProc.diagInSum / cnt
						avgRef := aecProc.diagRefSum / cnt
						avgOut := aecProc.diagOutSum / cnt
						reductionDB := math.NaN()
						if avgIn > 0 {
							reductionDB = 20 * math.Log10(avgOut / avgIn)
						}
						log.Printf("AEC: cb=%d avgIn=%.0f avgRef=%.0f peakRef=%.0f avgOut=%.0f reduction=%.1fdB frames=%d",
							callbackCount, avgIn, avgRef, aecProc.diagRefMax, avgOut, reductionDB, aecProc.diagCount)
						aecProc.diagInSum = 0
						aecProc.diagRefSum = 0
						aecProc.diagOutSum = 0
						aecProc.diagRefMax = 0
						aecProc.diagCount = 0
					}
				}

				if needInputResample {
					samplesToWrite = resampleS16(samplesToWrite, config.SampleRate, config.InputSampleRate)
				}
				_, err := w.Write(samplesToWrite)
				if err != nil {
					aborted = true
					abortChan <- err
					return
				}
			}
		},
	}

	return stream(ctx, abortChan, config, malgo.Duplex, deviceCallbacks)
}

// Playback streams samples from the provided reader to the playback device.
// The function initializes a playback device in the default context using the
// provided stream configuration.
// XXX: Capture, Duplex and Playback are mutually exclusive, only use one at a time
func Playback(ctx context.Context, r io.Reader, config StreamConfig) error {
	abortChan := make(chan error)
	defer close(abortChan)
	aborted := false

	deviceCallbacks := malgo.DeviceCallbacks{
		Data: func(outputSamples, inputSamples []byte, frameCount uint32) {
			if aborted {
				return
			}

			if len(outputSamples) > 0 {
				if frameCount == 0 {
					return
				}

				read, err := r.Read(outputSamples)
				if err != nil {
					aborted = true
					abortChan <- err
					return
				}
				for i := read; i < len(outputSamples); i++ {
					outputSamples[i] = 0
				}
			}
		},
	}

	return stream(ctx, abortChan, config, malgo.Playback, deviceCallbacks)
}
