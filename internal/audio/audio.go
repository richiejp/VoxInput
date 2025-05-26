package audio

import (
	"context"
	"io"

	"github.com/gen2brain/malgo"
)

// Copied from malgo/examples/io_api

// StreamConfig describes the parameters for an audio stream.
// Default values will pick the defaults of the default device.
type StreamConfig struct {
	Format       malgo.FormatType
	Channels     int
	SampleRate   int
	DeviceType   malgo.DeviceType
	MalgoContext malgo.Context
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
	if config.DeviceType != 0 {
		deviceConfig.DeviceType = config.DeviceType
	}
	return deviceConfig
}

func stream(ctx context.Context, abortChan chan error, config StreamConfig, deviceCallbacks malgo.DeviceCallbacks) error {
	deviceConfig := config.asDeviceConfig(malgo.Capture)
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

// Capture records incoming samples into the provided writer.
// The function initializes a capture device in the default context using
// provide stream configuration.
// Capturing will commence writing the samples to the writer until either the
// writer returns an error, or the context signals done.
func Capture(ctx context.Context, w io.Writer, config StreamConfig) error {
	config.DeviceType = malgo.Capture
	abortChan := make(chan error)
	defer close(abortChan)
	aborted := false

	deviceCallbacks := malgo.DeviceCallbacks{
		Data: func(outputSamples, inputSamples []byte, frameCount uint32) {
			if aborted {
				return
			}

			_, err := w.Write(inputSamples)
			if err != nil {
				aborted = true
				abortChan <- err
			}
		},
	}

	return stream(ctx, abortChan, config, deviceCallbacks)
}

// Playback streams samples from a reader to the sound device.
// The function initializes a playback device in the default context using
// provide stream configuration.
// Playback will commence playing the samples provided from the reader until either the
// reader returns an error, or the context signals done.
func Playback(ctx context.Context, r io.Reader, config StreamConfig) error {
	config.DeviceType = malgo.Playback
	abortChan := make(chan error)
	defer close(abortChan)
	aborted := false

	deviceCallbacks := malgo.DeviceCallbacks{
		Data: func(outputSamples, inputSamples []byte, frameCount uint32) {
			if aborted {
				return
			}
			if frameCount == 0 {
				return
			}

			read, err := io.ReadFull(r, outputSamples)
			if read <= 0 {
				if err != nil {
					aborted = true
					abortChan <- err
				}
				return
			}
		},
	}

	return stream(ctx, abortChan, config, deviceCallbacks)
}
