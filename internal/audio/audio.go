package audio

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/gen2brain/malgo"
)

// Copied from malgo/examples/io_api

// StreamConfig describes the parameters for an audio stream.
// Default values will pick the defaults of the default device.
type StreamConfig struct {
	Format          malgo.FormatType
	Channels        int
	SampleRate      int
	MalgoContext    malgo.Context
	CaptureDeviceID *malgo.DeviceID
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

// Duplex streams audio from a reader to the playback device and captures audio
// from the capture device to a writer.
// It initializes a duplex device in the default context using the provided stream configuration.
// It expects both r and w to be non-nil.
// XXX: Capture, Duplex and Playback are mutually exclusive, only use one at a time
func Duplex(ctx context.Context, r io.Reader, w io.Writer, config StreamConfig) error {
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
					return
				}
			}

			if len(outputSamples) > 0 {
				if frameCount == 0 {
					return
				}

				read, err := r.Read(outputSamples)
				if err != nil {
					if err == io.EOF {
						for i := read; i < len(outputSamples); i++ {
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
