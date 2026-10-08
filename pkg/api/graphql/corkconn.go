package graphql

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"sync"
)

// corkConn is the network connection of a WebSocket subscriber. Writes go
// straight to the connection, except while it is corked: then they collect
// in a buffer that Uncork sends with one write. gorilla/websocket writes
// every message (and control frame) with its own write, so a batch of
// frames costs one system call instead of one each (refactoring plan R5-5:
// the push path's CPU time was mostly those system calls).
type corkConn struct {
	net.Conn
	mu     sync.Mutex
	corked bool
	buf    []byte
}

// Write implements net.Conn.
func (c *corkConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.corked {
		c.buf = append(c.buf, p...)
		return len(p), nil
	}
	return c.Conn.Write(p)
}

// Cork holds the writes that follow until Uncork.
func (c *corkConn) Cork() {
	c.mu.Lock()
	c.corked = true
	c.mu.Unlock()
}

// maxIdleCorkBuffer bounds the buffer kept between batches.
const maxIdleCorkBuffer = 64 << 10

// Uncork sends the held writes with one write (bounded by the write
// deadline last set on the connection) and lets later writes through.
func (c *corkConn) Uncork() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.corked = false
	if len(c.buf) == 0 {
		return nil
	}
	_, err := c.Conn.Write(c.buf)
	if cap(c.buf) > maxIdleCorkBuffer {
		c.buf = nil
	} else {
		c.buf = c.buf[:0]
	}
	return err
}

// corkWriter hands the WebSocket upgrade a corkConn when it hijacks the
// HTTP connection.
type corkWriter struct {
	http.ResponseWriter
	conn *corkConn
}

// Hijack implements http.Hijacker.
func (w *corkWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("the response writer cannot be hijacked")
	}
	c, rw, err := h.Hijack()
	if err != nil {
		return nil, nil, err
	}
	w.conn = &corkConn{Conn: c}
	return w.conn, rw, nil
}
