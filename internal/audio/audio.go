package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/gen2brain/malgo"
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
	MalgoContext      malgo.Context
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

// CaptureToRing pushes captured samples into the provided ring buffer as
// int16. Intended to run concurrently with a Duplex stream so the ring
// can feed a reference signal (e.g. a speaker monitor / loopback device)
// to an AEC processor. Unlike Capture, this uses its own malgo context
// so the caller does not need to share one.
func CaptureToRing(ctx context.Context, ring *Int16Ring, config StreamConfig) error {
	abortChan := make(chan error)
	defer close(abortChan)
	aborted := false

	deviceCallbacks := malgo.DeviceCallbacks{
		Data: func(outputSamples, inputSamples []byte, frameCount uint32) {
			if aborted {
				return
			}
			if len(inputSamples) == 0 {
				return
			}
			ring.Write(bytesToS16(inputSamples))
		},
	}

	return stream(ctx, abortChan, config, malgo.Capture, deviceCallbacks)
}

// resampleS16 is an allocating wrapper over resampleS16Into, kept for tests
// and non-hot-path callers.
func resampleS16(src []byte, fromRate, toRate int) []byte {
	if fromRate == toRate {
		out := make([]byte, len(src))
		copy(out, src)
		return out
	}
	numSamples := len(src) / 2
	ratio := float64(fromRate) / float64(toRate)
	newNumSamples := int(float64(numSamples) / ratio)
	out := make([]byte, newNumSamples*2)
	resampleS16Into(out, src, fromRate, toRate)
	return out
}

// resampleS16Into resamples src (int16 LE bytes) into dst at the requested
// rate change using linear interpolation. Returns the number of bytes written.
func resampleS16Into(dst, src []byte, fromRate, toRate int) int {
	if fromRate == toRate {
		n := copy(dst, src)
		return n
	}

	numSamples := len(src) / 2
	ratio := float64(fromRate) / float64(toRate)
	newNumSamples := int(float64(numSamples) / ratio)
	if newNumSamples*2 > len(dst) {
		newNumSamples = len(dst) / 2
	}

	for i := range newNumSamples {
		srcPos := float64(i) * ratio
		srcIdx := int(srcPos)
		frac := srcPos - float64(srcIdx)

		if srcIdx >= numSamples-1 {
			binary.LittleEndian.PutUint16(dst[i*2:], binary.LittleEndian.Uint16(src[(numSamples-1)*2:]))
			continue
		}

		sample1 := int16(binary.LittleEndian.Uint16(src[srcIdx*2:]))
		sample2 := int16(binary.LittleEndian.Uint16(src[(srcIdx+1)*2:]))

		interpolated := int16(float64(sample1)*(1-frac) + float64(sample2)*frac)
		binary.LittleEndian.PutUint16(dst[i*2:], uint16(interpolated))
	}

	return newNumSamples * 2
}

// AudioProcessor processes captured audio with a playback reference.
// Process takes rec (microphone) and play (speaker) byte slices of int16 LE
// PCM at the device sample rate plus a caller-owned out buffer, and writes
// cleaned samples into out. It returns the number of bytes written (0 if still
// buffering). Implementations must not allocate in the steady state.
type AudioProcessor interface {
	Process(rec, play, out []byte) int
}

// DuplexOpts holds optional parameters for Duplex.
type DuplexOpts struct {
	// Raw mic samples at the hardware sample rate, before AEC.
	DumpInput io.Writer
	// Reference signal as seen by the AEC processor at the hardware sample rate.
	// In playback-ref mode this equals the far-end TTS buffer fed to the speaker;
	// in monitor-ref mode this equals the monitor-capture samples.
	DumpOutput io.Writer
	// Cleaned mic samples at the hardware sample rate, after AEC processing.
	// Written from the AEC worker goroutine when AECMicRing/AECRefRing are set;
	// otherwise written inline from the audio callback.
	DumpProcessed io.Writer
	// Far-end TTS samples we sent to the speaker. Only populated when
	// using a monitor ref source, so post-hoc tools can compare what we
	// intended to play against what the monitor actually captured.
	DumpTTS io.Writer
	// Optional ring buffer of int16 samples at the hardware sample rate.
	// When non-nil, Duplex pulls the AEC reference from this ring
	// instead of using the TTS buffer it sent to the speaker.
	RefSource *Int16Ring
	// When both are non-nil, Duplex's audio callback enqueues mic + reference
	// samples onto these rings instead of running processor.Process inline.
	// A separate AEC worker goroutine (see NewAECWorker) is expected to drain
	// the rings and produce cleaned samples. Keeps neural inference off the
	// realtime audio thread.
	AECMicRing *Int16Ring
	AECRefRing *Int16Ring
}

// Duplex streams audio from r to the playback device and captures audio from
// the capture device to w.
// It initializes a duplex device in the default context using the provided
// stream configuration. Both r and w must be non-nil.
// If InputSampleRate and OutputSampleRate differ from SampleRate, resampling
// is performed inline (zero alloc after the first callback).
// If processor is non-nil and opts.AECMicRing/AECRefRing are nil, the speaker
// output is used as reference for echo cancellation *inline* in the audio
// callback (legacy path, kept for tests). When opts.AECMicRing and
// opts.AECRefRing are both set, the callback only enqueues mic+ref onto those
// rings — an AEC worker goroutine (see NewAECWorker) is expected to drain
// them, run the processor, and write cleaned bytes to w.
// XXX: Capture, Duplex and Playback are mutually exclusive.
func Duplex(ctx context.Context, r io.Reader, w io.Writer, config StreamConfig, processor AudioProcessor, opts *DuplexOpts) error {
	abortChan := make(chan error)
	defer close(abortChan)
	aborted := false

	needInputResample := config.InputSampleRate != 0 && config.InputSampleRate != config.SampleRate
	needOutputResample := config.OutputSampleRate != 0 && config.OutputSampleRate != config.SampleRate

	delegateToWorker := opts != nil && opts.AECMicRing != nil && opts.AECRefRing != nil

	// Preallocate every scratch we might touch in the callback. Sizes come
	// from the configured period; malgo normally honours PeriodMs exactly.
	// A 2x safety factor absorbs any jitter the driver throws at us.
	periodMs := config.PeriodMs
	if periodMs == 0 {
		periodMs = 20
	}
	maxDeviceSamples := 2 * periodMs * config.SampleRate / 1000
	if maxDeviceSamples < 1 {
		maxDeviceSamples = 1024
	}
	maxDeviceBytes := maxDeviceSamples * 2

	readBuf := make([]byte, maxDeviceBytes)
	refScratch := make([]int16, maxDeviceSamples)
	refBytes := make([]byte, maxDeviceBytes)
	processedOut := make([]byte, maxDeviceBytes)
	// +2 bytes of slack for rounding when resampling up.
	resampledInput := make([]byte, maxDeviceBytes+2)
	micInt16 := make([]int16, maxDeviceSamples)

	callbackCount := 0
	var diagInputBytes, diagWrittenBytes, diagProcessorNil int

	deviceCallbacks := malgo.DeviceCallbacks{
		Data: func(outputSamples, inputSamples []byte, frameCount uint32) {
			if aborted {
				return
			}
			callbackCount++

			// Handle the speaker buffer first so refSamples is ready before
			// we touch the capture side.
			if len(outputSamples) > 0 {
				if frameCount == 0 {
					return
				}

				bytesToRead := len(outputSamples)
				if needOutputResample {
					ratio := float64(config.OutputSampleRate) / float64(config.SampleRate)
					bytesToRead = int(float64(len(outputSamples)) * ratio)
				}
				if bytesToRead > cap(readBuf) {
					readBuf = make([]byte, bytesToRead)
				}
				readBuf = readBuf[:bytesToRead]

				read, err := r.Read(readBuf)
				if err != nil {
					if err == io.EOF {
						for i := range outputSamples {
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
					n := resampleS16Into(outputSamples, readBuf[:read], config.OutputSampleRate, config.SampleRate)
					for i := n; i < len(outputSamples); i++ {
						outputSamples[i] = 0
					}
				} else {
					copy(outputSamples, readBuf[:read])
					for i := read; i < len(outputSamples); i++ {
						outputSamples[i] = 0
					}
				}
			}

			// Pick the AEC reference: what we pushed to the speaker, or (in
			// monitor-ref mode) the samples captured off a monitor device.
			refSamples := outputSamples
			if opts != nil && opts.RefSource != nil && len(outputSamples) > 0 {
				n := len(outputSamples) / 2
				if n > cap(refScratch) {
					refScratch = make([]int16, n)
					refBytes = make([]byte, n*2)
				}
				refScratch = refScratch[:n]
				refBytes = refBytes[:n*2]
				opts.RefSource.Read(refScratch)
				s16ToBytesInto(refBytes, refScratch)
				refSamples = refBytes
			}

			if opts != nil && opts.DumpOutput != nil && len(refSamples) > 0 {
				opts.DumpOutput.Write(refSamples)
			}
			if opts != nil && opts.DumpTTS != nil && len(outputSamples) > 0 {
				opts.DumpTTS.Write(outputSamples)
			}

			if len(inputSamples) > 0 {
				if opts != nil && opts.DumpInput != nil {
					opts.DumpInput.Write(inputSamples)
				}

				diagInputBytes += len(inputSamples)

				// Fast path: hand mic+ref to the worker via rings and return.
				if delegateToWorker && len(refSamples) > 0 {
					n := len(inputSamples) / 2
					if n > cap(micInt16) {
						micInt16 = make([]int16, n)
					}
					micInt16 = micInt16[:n]
					bytesToS16Into(micInt16, inputSamples)
					opts.AECMicRing.Write(micInt16)

					nr := len(refSamples) / 2
					if nr > cap(refScratch) {
						refScratch = make([]int16, nr)
					}
					refScratch = refScratch[:nr]
					bytesToS16Into(refScratch, refSamples)
					opts.AECRefRing.Write(refScratch)
				} else {
					// Legacy inline AEC path (processor runs in callback).
					samplesToWrite := inputSamples

					if processor != nil && len(refSamples) > 0 {
						// Pass the full preallocated capacity: the processor
						// may emit more bytes than it consumed in a single
						// call when prior accumulator samples drain.
						processedOut = processedOut[:cap(processedOut)]
						n := processor.Process(inputSamples, refSamples, processedOut)
						if n > 0 {
							samplesToWrite = processedOut[:n]
							if opts != nil && opts.DumpProcessed != nil {
								opts.DumpProcessed.Write(samplesToWrite)
							}
						} else {
							diagProcessorNil++
							samplesToWrite = nil
						}
					}

					if len(samplesToWrite) > 0 {
						if needInputResample {
							need := len(samplesToWrite)*config.InputSampleRate/config.SampleRate + 2
							if need > cap(resampledInput) {
								resampledInput = make([]byte, need)
							}
							resampledInput = resampledInput[:cap(resampledInput)]
							n := resampleS16Into(resampledInput, samplesToWrite, config.SampleRate, config.InputSampleRate)
							samplesToWrite = resampledInput[:n]
						}
						diagWrittenBytes += len(samplesToWrite)
						_, err := w.Write(samplesToWrite)
						if err != nil {
							aborted = true
							abortChan <- err
							return
						}
					}
				}
			}

			if callbackCount%100 == 0 {
				log.Printf("Duplex: callbacks=%d inputBytes=%d writtenBytes=%d processorNil=%d",
					callbackCount, diagInputBytes, diagWrittenBytes, diagProcessorNil)
			}
		},
	}

	return stream(ctx, abortChan, config, malgo.Duplex, deviceCallbacks)
}

// AECWorkerOpts configures NewAECWorker.
type AECWorkerOpts struct {
	// Mirror of cleaned samples at the device sample rate (pre-output-resample),
	// for offline AEC analysis.
	DumpProcessed io.Writer
	// Poll interval for draining the mic/ref rings. Defaults to 5ms.
	TickInterval time.Duration
}

// AECWorker drains mic+ref rings, runs processor, and writes cleaned samples
// (resampled to outputRate when that differs from deviceRate) to out. All
// scratch is preallocated at construction so the run loop is zero-alloc in
// the steady state.
type AECWorker struct {
	ctx          context.Context
	processor    AudioProcessor
	micRing      *Int16Ring
	refRing      *Int16Ring
	deviceRate   int
	outputRate   int
	out          io.Writer
	opts         *AECWorkerOpts
	batchSamples int

	micInt16    []int16
	refInt16    []int16
	micBytes    []byte
	refBytes    []byte
	cleanedBuf  []byte
	resampleOut []byte

	done chan struct{}
}

// NewAECWorker spawns a goroutine that reads mic/ref samples from the rings,
// runs processor.Process, and writes cleaned bytes to out. deviceRate is the
// sample rate carried by the rings; outputRate is the rate expected by out
// (typically the upstream encoder's input rate). The worker exits when ctx is
// done.
func NewAECWorker(
	ctx context.Context,
	processor AudioProcessor,
	micRing, refRing *Int16Ring,
	deviceRate, outputRate int,
	out io.Writer,
	opts *AECWorkerOpts,
) *AECWorker {
	// One 20 ms batch at deviceRate per drain iteration matches the typical
	// audio callback period, so the worker keeps the rings shallow.
	batchSamples := deviceRate / 50
	if batchSamples < 1 {
		batchSamples = 1
	}

	// Processor output per call can exceed input when accumulated hop
	// remainders drain and/or when resampling up. Size cleanedBuf to 2x
	// the input batch in bytes which covers all realistic cases.
	cleanedBytes := batchSamples * 4
	resampleOutBytes := (cleanedBytes*outputRate+deviceRate-1)/deviceRate + 2
	if resampleOutBytes < cleanedBytes {
		resampleOutBytes = cleanedBytes
	}

	w := &AECWorker{
		ctx:          ctx,
		processor:    processor,
		micRing:      micRing,
		refRing:      refRing,
		deviceRate:   deviceRate,
		outputRate:   outputRate,
		out:          out,
		opts:         opts,
		batchSamples: batchSamples,

		micInt16:    make([]int16, batchSamples),
		refInt16:    make([]int16, batchSamples),
		micBytes:    make([]byte, batchSamples*2),
		refBytes:    make([]byte, batchSamples*2),
		cleanedBuf:  make([]byte, cleanedBytes),
		resampleOut: make([]byte, resampleOutBytes),

		done: make(chan struct{}),
	}
	go w.run()
	return w
}

// Done returns a channel closed when the worker's run loop exits.
func (w *AECWorker) Done() <-chan struct{} {
	return w.done
}

func (w *AECWorker) run() {
	defer close(w.done)

	interval := 5 * time.Millisecond
	if w.opts != nil && w.opts.TickInterval > 0 {
		interval = w.opts.TickInterval
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-tick.C:
			w.drain()
		}
	}
}

func (w *AECWorker) drain() {
	for w.micRing.Len() >= w.batchSamples && w.refRing.Len() >= w.batchSamples {
		w.micRing.Read(w.micInt16)
		w.refRing.Read(w.refInt16)
		s16ToBytesInto(w.micBytes, w.micInt16)
		s16ToBytesInto(w.refBytes, w.refInt16)

		n := w.processor.Process(w.micBytes, w.refBytes, w.cleanedBuf)
		if n == 0 {
			continue
		}
		cleaned := w.cleanedBuf[:n]

		if w.opts != nil && w.opts.DumpProcessed != nil {
			w.opts.DumpProcessed.Write(cleaned)
		}

		if w.outputRate != 0 && w.outputRate != w.deviceRate {
			need := n*w.outputRate/w.deviceRate + 2
			if need > cap(w.resampleOut) {
				w.resampleOut = make([]byte, need)
			}
			w.resampleOut = w.resampleOut[:cap(w.resampleOut)]
			m := resampleS16Into(w.resampleOut, cleaned, w.deviceRate, w.outputRate)
			cleaned = w.resampleOut[:m]
		}

		if _, err := w.out.Write(cleaned); err != nil {
			log.Printf("AECWorker: write error: %v", err)
			return
		}
	}
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
