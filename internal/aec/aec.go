package aec

/*
#cgo pkg-config: speexdsp
#include <speex/speex_echo.h>
#include <speex/speex_preprocess.h>
*/
import "C"
import (
	"fmt"
	"unsafe"
)

// Canceller wraps SpeexDSP's acoustic echo cancellation state
// and an optional preprocessor for residual echo suppression.
type Canceller struct {
	state     *C.SpeexEchoState
	preproc   *C.SpeexPreprocessState
	frameSize int
}

// New creates a new echo canceller.
// frameSize is the number of samples per frame (e.g. sampleRate * 20 / 1000 for 20ms).
// filterLen is the length of the echo tail in samples (e.g. sampleRate * 500 / 1000 for 500ms).
func New(frameSize, filterLen, sampleRate int) (*Canceller, error) {
	state := C.speex_echo_state_init(C.int(frameSize), C.int(filterLen))
	if state == nil {
		return nil, fmt.Errorf("speex_echo_state_init failed (frameSize=%d, filterLen=%d)", frameSize, filterLen)
	}
	rate := C.spx_int32_t(sampleRate)
	C.speex_echo_ctl(state, C.SPEEX_ECHO_SET_SAMPLING_RATE, unsafe.Pointer(&rate))

	preproc := C.speex_preprocess_state_init(C.int(frameSize), C.int(sampleRate))
	if preproc == nil {
		C.speex_echo_state_destroy(state)
		return nil, fmt.Errorf("speex_preprocess_state_init failed")
	}
	C.speex_preprocess_ctl(preproc, C.SPEEX_PREPROCESS_SET_ECHO_STATE, unsafe.Pointer(state))

	return &Canceller{state: state, preproc: preproc, frameSize: frameSize}, nil
}

// Process runs echo cancellation followed by residual echo suppression.
// rec is the recorded microphone signal (input).
// play is the signal played to the speaker (reference).
// out receives the cleaned signal.
// All slices are raw int16 little-endian PCM bytes and must be len == frameSize*2.
func (c *Canceller) Process(rec, play, out []byte) {
	C.speex_echo_cancellation(
		c.state,
		(*C.spx_int16_t)(unsafe.Pointer(&rec[0])),
		(*C.spx_int16_t)(unsafe.Pointer(&play[0])),
		(*C.spx_int16_t)(unsafe.Pointer(&out[0])),
	)
	if c.preproc != nil {
		C.speex_preprocess_run(c.preproc, (*C.spx_int16_t)(unsafe.Pointer(&out[0])))
	}
}

// FrameSize returns the number of samples per frame.
func (c *Canceller) FrameSize() int {
	return c.frameSize
}

// Destroy frees the echo canceller and preprocessor state.
func (c *Canceller) Destroy() {
	if c.preproc != nil {
		C.speex_preprocess_state_destroy(c.preproc)
		c.preproc = nil
	}
	if c.state != nil {
		C.speex_echo_state_destroy(c.state)
		c.state = nil
	}
}
