// SPDX-License-Identifier: GPL-2.0-only
package boxtransport

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

type fragmented struct{ io.Reader }

func (r fragmented) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.Reader.Read(p)
}
func TestFragmentedFramesAndDispatch(t *testing.T) {
	wire := []byte{0, 0, 0, 2, 0x12, 0, 0, 0, 0, 2, 0x1a, 0}
	reader := bufio.NewReader(fragmented{bytes.NewReader(wire)})
	protocol, err := Detect(reader)
	if err != nil || protocol != RTNL {
		t.Fatalf("detect: %v %v", protocol, err)
	}
	var frames [][]byte
	if err := ReadFrames(reader, func(frame []byte) error { frames = append(frames, frame); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || !bytes.Equal(frames[0], []byte{0x12, 0}) || !bytes.Equal(frames[1], []byte{0x1a, 0}) {
		t.Fatalf("frames: %x", frames)
	}
	reader = bufio.NewReader(fragmented{bytes.NewBufferString("GET /v1/time HTTP/1.1\r\n\r\n")})
	protocol, err = Detect(reader)
	if err != nil || protocol != HTTP {
		t.Fatal(protocol, err)
	}
	remaining, _ := io.ReadAll(reader)
	if !bytes.HasPrefix(remaining, []byte("GET ")) {
		t.Fatal("detection dropped HTTP bytes")
	}
}
func TestInvalidFrameLengths(t *testing.T) {
	for _, n := range []uint32{0, 65536, 0xffffffff} {
		var header [4]byte
		binary.BigEndian.PutUint32(header[:], n)
		if _, err := ReadFrame(bytes.NewReader(header[:])); err == nil {
			t.Fatalf("length %d accepted", n)
		}
	}
	for _, wire := range [][]byte{{0}, {0, 0, 0, 2, 1}} {
		if _, err := ReadFrame(bytes.NewReader(wire)); err == nil {
			t.Fatal("truncation accepted")
		}
	}
}
