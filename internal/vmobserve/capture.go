package vmobserve

import (
	"context"
	"errors"
	"sync"
)

var errCapture = errors.New("vm observation: closed or excessive output")

// Stderr is counted and discarded, never stored or classified by its text.
type capture struct {
	mu                     sync.Mutex
	data                   []byte
	max, used              int
	keep, overflow, closed bool
	cancel                 context.CancelFunc
}

func newCapture(max int, keep bool, cancel context.CancelFunc) *capture {
	return &capture{max: max, keep: keep, cancel: cancel}
}
func (c *capture) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, errCapture
	}
	n := min(len(data), c.max-c.used)
	if c.keep {
		c.data = append(c.data, data[:n]...)
	}
	c.used += n
	if n < len(data) {
		c.overflow = true
		c.cancel()
		return n, errCapture
	}
	return n, nil
}
func (c *capture) seal() ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	v := append([]byte(nil), c.data...)
	clear(c.data)
	c.data = nil
	return v, c.overflow
}
