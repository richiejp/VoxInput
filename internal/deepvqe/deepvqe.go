package deepvqe

import (
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
)

type DeepVQE struct {
	lib uintptr
	ctx uintptr

	fnFree       func(uintptr)
	fnProcessS16 func(uintptr, uintptr, uintptr, int32, uintptr) int32
	fnLastError  func(uintptr) uintptr
	fnSampleRate func(uintptr) int32
}

// New loads the shared library and model.
func New(libPath, modelPath string) (*DeepVQE, error) {
	lib, err := purego.Dlopen(libPath, purego.RTLD_LAZY)
	if err != nil {
		return nil, fmt.Errorf("dlopen %s: %w", libPath, err)
	}

	d := &DeepVQE{lib: lib}

	var fnNew func(uintptr) uintptr
	purego.RegisterLibFunc(&fnNew, lib, "deepvqe_new")
	purego.RegisterLibFunc(&d.fnFree, lib, "deepvqe_free")
	purego.RegisterLibFunc(&d.fnProcessS16, lib, "deepvqe_process_s16")
	purego.RegisterLibFunc(&d.fnLastError, lib, "deepvqe_last_error")
	purego.RegisterLibFunc(&d.fnSampleRate, lib, "deepvqe_sample_rate")

	pathBytes := append([]byte(modelPath), 0) // null-terminated
	d.ctx = fnNew(uintptr(unsafe.Pointer(&pathBytes[0])))
	if d.ctx == 0 {
		purego.Dlclose(lib)
		return nil, fmt.Errorf("deepvqe_new failed for %s", modelPath)
	}

	return d, nil
}

// ProcessS16 runs AEC on int16 PCM buffers (16kHz mono).
func (d *DeepVQE) ProcessS16(mic, ref []int16) ([]int16, error) {
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
		return nil, fmt.Errorf("deepvqe_process_s16 error %d: %s", ret, d.LastError())
	}
	return out, nil
}

func (d *DeepVQE) LastError() string {
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
func (d *DeepVQE) SampleRate() int {
	return int(d.fnSampleRate(d.ctx))
}

// Close frees the context and unloads the library.
func (d *DeepVQE) Close() {
	if d.ctx != 0 {
		d.fnFree(d.ctx)
		d.ctx = 0
	}
	if d.lib != 0 {
		purego.Dlclose(d.lib)
		d.lib = 0
	}
}
