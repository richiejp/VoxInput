package localvqe

import (
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
)

type LocalVQE struct {
	lib uintptr
	ctx uintptr

	fnFree           func(uintptr)
	fnProcessS16     func(uintptr, uintptr, uintptr, int32, uintptr) int32
	fnProcessFrameS16 func(uintptr, uintptr, uintptr, int32, uintptr) int32
	fnReset          func(uintptr)
	fnLastError      func(uintptr) uintptr
	fnSampleRate     func(uintptr) int32
	fnHopLength      func(uintptr) int32
}

// New loads the shared library and model.
func New(libPath, modelPath string) (*LocalVQE, error) {
	lib, err := purego.Dlopen(libPath, purego.RTLD_LAZY)
	if err != nil {
		return nil, fmt.Errorf("dlopen %s: %w", libPath, err)
	}

	d := &LocalVQE{lib: lib}

	var fnNew func(uintptr) uintptr
	purego.RegisterLibFunc(&fnNew, lib, "localvqe_new")
	purego.RegisterLibFunc(&d.fnFree, lib, "localvqe_free")
	purego.RegisterLibFunc(&d.fnProcessS16, lib, "localvqe_process_s16")
	purego.RegisterLibFunc(&d.fnProcessFrameS16, lib, "localvqe_process_frame_s16")
	purego.RegisterLibFunc(&d.fnReset, lib, "localvqe_reset")
	purego.RegisterLibFunc(&d.fnLastError, lib, "localvqe_last_error")
	purego.RegisterLibFunc(&d.fnSampleRate, lib, "localvqe_sample_rate")
	purego.RegisterLibFunc(&d.fnHopLength, lib, "localvqe_hop_length")

	pathBytes := append([]byte(modelPath), 0) // null-terminated
	d.ctx = fnNew(uintptr(unsafe.Pointer(&pathBytes[0])))
	if d.ctx == 0 {
		purego.Dlclose(lib)
		return nil, fmt.Errorf("localvqe_new failed for %s", modelPath)
	}

	return d, nil
}

// ProcessS16 runs AEC on int16 PCM buffers (16kHz mono).
func (d *LocalVQE) ProcessS16(mic, ref []int16) ([]int16, error) {
	n := int32(len(mic))
	out := make([]int16, n)
	ret := d.fnProcessS16(
		d.ctx,
		uintptr(unsafe.Pointer(&mic[0])),
		uintptr(unsafe.Pointer(&ref[0])),
		n,
		uintptr(unsafe.Pointer(&out[0])),
	)
	if ret != 0 {
		return nil, fmt.Errorf("localvqe_process_s16 error %d: %s", ret, d.LastError())
	}
	return out, nil
}

// ProcessFrameS16 processes a single hop of int16 PCM (16kHz mono).
// mic and ref must each have exactly HopLength() samples.
func (d *LocalVQE) ProcessFrameS16(mic, ref []int16) ([]int16, error) {
	n := int32(len(mic))
	out := make([]int16, n)
	ret := d.fnProcessFrameS16(
		d.ctx,
		uintptr(unsafe.Pointer(&mic[0])),
		uintptr(unsafe.Pointer(&ref[0])),
		n,
		uintptr(unsafe.Pointer(&out[0])),
	)
	if ret != 0 {
		return nil, fmt.Errorf("localvqe_process_frame_s16 error %d: %s", ret, d.LastError())
	}
	return out, nil
}

// Reset clears streaming state (overlap buffers, GRU hidden state).
func (d *LocalVQE) Reset() {
	d.fnReset(d.ctx)
}

// HopLength returns the model hop length in samples.
func (d *LocalVQE) HopLength() int {
	return int(d.fnHopLength(d.ctx))
}

func (d *LocalVQE) LastError() string {
	ptr := d.fnLastError(d.ctx)
	if ptr == 0 {
		return ""
	}
	// Read null-terminated C string using unsafe.Add (avoids go vet
	// false positive on uintptr arithmetic with unsafe.Pointer).
	p := unsafe.Pointer(ptr)
	var buf []byte
	for i := 0; ; i++ {
		b := *(*byte)(unsafe.Add(p, i))
		if b == 0 {
			break
		}
		buf = append(buf, b)
	}
	return string(buf)
}

// SampleRate returns the model sample rate (16000).
func (d *LocalVQE) SampleRate() int {
	return int(d.fnSampleRate(d.ctx))
}

// Close frees the context and unloads the library.
func (d *LocalVQE) Close() {
	if d.ctx != 0 {
		d.fnFree(d.ctx)
		d.ctx = 0
	}
	if d.lib != 0 {
		purego.Dlclose(d.lib)
		d.lib = 0
	}
}
