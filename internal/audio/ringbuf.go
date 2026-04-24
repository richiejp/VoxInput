package audio

import "sync"

// Int16Ring is a thread-safe bounded FIFO for int16 samples. On overrun
// the oldest samples are discarded; on underrun Read zero-fills the
// remainder of the destination slice. Intended for decoupling independent
// audio callbacks (e.g. a monitor capture stream feeding the duplex AEC
// reference).
type Int16Ring struct {
	mu   sync.Mutex
	buf  []int16
	head int
	tail int
	size int
	cap  int
}

func NewInt16Ring(capacity int) *Int16Ring {
	return &Int16Ring{
		buf: make([]int16, capacity),
		cap: capacity,
	}
}

// Write appends samples, dropping the oldest on overrun. Returns the
// number of samples written (always len(s) unless capacity is zero).
func (r *Int16Ring) Write(s []int16) int {
	if len(s) == 0 || r.cap == 0 {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	// If the incoming batch alone exceeds capacity, keep only the tail.
	if len(s) >= r.cap {
		copy(r.buf, s[len(s)-r.cap:])
		r.head = 0
		r.tail = 0
		r.size = r.cap
		return len(s)
	}

	for _, v := range s {
		r.buf[r.tail] = v
		r.tail = (r.tail + 1) % r.cap
		if r.size == r.cap {
			r.head = (r.head + 1) % r.cap
		} else {
			r.size++
		}
	}
	return len(s)
}

// Read fills dst with samples, zero-padding any shortfall. The returned
// count is the number of real (non-padded) samples copied.
func (r *Int16Ring) Read(dst []int16) int {
	if len(dst) == 0 {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	n := min(len(dst), r.size)
	for i := range n {
		dst[i] = r.buf[r.head]
		r.head = (r.head + 1) % r.cap
	}
	r.size -= n
	for i := n; i < len(dst); i++ {
		dst[i] = 0
	}
	return n
}

func (r *Int16Ring) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.size
}

func (r *Int16Ring) Cap() int {
	return r.cap
}
