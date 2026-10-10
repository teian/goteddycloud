// SPDX-License-Identifier: GPL-2.0-only
// Framing derived from src/handler_rtnl.c; see NOTICE for upstream attribution.
package boxtransport

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
)

const MaxRTNLFrame = 65535

type Protocol int

const (
	HTTP Protocol = iota
	RTNL
)

// Detect preserves all bytes for the selected consumer. The pinned C dispatch
// recognizes binary by its first two zero bytes; length checks happen before
// allocation in ReadFrame. Deadlines/cancellation belong to the caller.
func Detect(r *bufio.Reader) (Protocol, error) {
	prefix, err := r.Peek(2)
	if err != nil {
		return HTTP, err
	}
	if prefix[0] == 0 && prefix[1] == 0 {
		return RTNL, nil
	}
	return HTTP, nil
}
func ReadFrame(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(header[:])
	if n == 0 || n > MaxRTNLFrame {
		return nil, fmt.Errorf("invalid RTNL frame length %d", n)
	}
	data := make([]byte, int(n))
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	return data, nil
}
func ReadFrames(r io.Reader, receive func([]byte) error) error {
	if receive == nil {
		return errors.New("missing frame consumer")
	}
	for {
		frame, err := ReadFrame(r)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := receive(frame); err != nil {
			return err
		}
	}
}

// BufferedConn hands a detected HTTP connection to net/http without dropping
// the bytes buffered during protocol detection.
type BufferedConn struct {
	net.Conn
	Reader *bufio.Reader
}

func (c *BufferedConn) Read(p []byte) (int, error) { return c.Reader.Read(p) }
