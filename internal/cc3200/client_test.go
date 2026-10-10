// SPDX-License-Identifier: GPL-2.0-only
package cc3200

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"
)

type fakeLink struct {
	writeFlags                   uint32
	buffer                       []byte
	files                        map[string][]byte
	flash                        []byte
	sram                         []byte
	target                       string
	writing                      bool
	staged                       []byte
	writes                       []int
	commands                     []byte
	flags                        uint32
	corruptChecksum, corruptRead bool
	application                  bool
	block                        chan struct{}
	entered                      chan struct{}
}

func newFake() *fakeLink {
	return &fakeLink{files: map[string][]byte{}, flash: make([]byte, 6*BlockSize), sram: make([]byte, 128<<10)}
}
func (f *fakeLink) Read(ctx context.Context, n int, _ time.Duration) ([]byte, error) {
	if f.block != nil {
		select {
		case f.entered <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.block:
		}
	}
	if len(f.buffer) < n {
		return nil, io.ErrUnexpectedEOF
	}
	data := append([]byte(nil), f.buffer[:n]...)
	f.buffer = f.buffer[n:]
	return data, nil
}
func (f *fakeLink) Flush(context.Context) error       { f.buffer = nil; return nil }
func (f *fakeLink) Break(context.Context) error       { f.buffer = append(f.buffer, 0, 0xcc); return nil }
func (f *fakeLink) Reset(context.Context, bool) error { return nil }
func (f *fakeLink) Write(_ context.Context, frame []byte) error {
	if bytes.Equal(frame, []byte{0, 0xcc}) {
		return nil
	}
	if len(frame) < 4 || int(binary.BigEndian.Uint16(frame)) != len(frame)-1 || checksum(frame[3:]) != frame[2] {
		return errors.New("invalid command framing")
	}
	data := frame[3:]
	op := data[0]
	body := data[1:]
	f.commands = append(f.commands, op)
	word := func(at int) int { return int(binary.BigEndian.Uint32(body[at:])) }
	var response []byte
	raw := false
	switch op {
	case 0x2f:
		response = make([]byte, 28)
		response[1] = 4
		if f.application {
			response[16] = 16
		}
	case 0x27:
		response = []byte{0x84}
		raw = true
	case 0x31:
		response = make([]byte, 4)
		binary.BigEndian.PutUint16(response, BlockSize)
		binary.BigEndian.PutUint16(response[2:], uint16(len(f.flash)/BlockSize))
	case 0x2c:
		response = append([]byte(nil), f.flash[word(4):word(4)+word(8)]...)
	case 0x2d:
		offset := word(4)
		size := word(8)
		if len(body[12:]) != size {
			return errors.New("invalid raw write length")
		}
		if word(0) == 0 {
			copy(f.sram[offset:], body[12:])
		} else {
			copy(f.flash[offset:], body[12:])
			f.writes = append(f.writes, offset)
		}
	case 0x30:
		clear(f.flash)
	case 0x33:
		if word(0) != 26666667 {
			return errors.New("invalid handover clock")
		}
	case 0x32: // ACK below plus loader execution ACK.
	case 0x2a:
		name := string(body[4:])
		if word(0) != len(name) {
			return errors.New("invalid filename length")
		}
		response = make([]byte, 8)
		if content, ok := f.files[name]; ok {
			response[0] = 1
			binary.BigEndian.PutUint32(response[4:], uint32(len(content)))
		}
	case 0x21:
		f.target = string(body[8 : len(body)-2])
		f.flags = uint32(word(0))
		if f.flags != 0 {
			f.writeFlags = f.flags
		}
		f.writing = f.flags != 0
		f.staged = nil
		response = make([]byte, 4)
		raw = true
	case 0x22:
		if f.writing {
			f.files[f.target] = append([]byte(nil), f.staged...)
		}
	case 0x23:
		response = []byte{0x40}
	case 0x24:
		if word(0) != len(f.staged) {
			return errors.New("invalid file offset")
		}
		f.staged = append(f.staged, body[4:]...)
	case 0x2b:
		response = append([]byte(nil), f.files[f.target][word(0):word(0)+word(4)]...)
		if f.corruptRead && len(response) > 0 {
			response[0] ^= 1
		}
	case 0x2e:
		delete(f.files, string(body[4:len(body)-1]))
	case 0x28:
		if word(0) != 2 || word(4) > len(f.flash)/BlockSize || word(16) != 2 {
			return errors.New("invalid format command")
		}
		clear(f.files)
	default:
		return fmt.Errorf("unexpected opcode %x", op)
	}
	f.buffer = append(f.buffer, 0, 0xcc)
	if response != nil {
		if raw {
			f.buffer = append(f.buffer, response...)
		} else {
			header := make([]byte, 3)
			binary.BigEndian.PutUint16(header, uint16(len(response)+2))
			header[2] = checksum(response)
			if f.corruptChecksum {
				header[2]++
			}
			f.buffer = append(f.buffer, header...)
			f.buffer = append(f.buffer, response...)
		}
	}
	if op == 0x32 {
		f.buffer = append(f.buffer, 0, 0xcc)
	}
	return nil
}
func TestClientFileRoundTripAndVerification(t *testing.T) {
	f := newFake()
	f.files["/cert/c2.der"] = []byte{99}
	client := NewClient(f)
	ctx := context.Background()
	payload := bytes.Repeat([]byte{1, 2, 3, 4, 5, 6, 7}, 1301)
	if err := client.WriteFile(ctx, "/cert/c2.der", payload, WriteOptions{Verify: true, Commit: true}); err != nil {
		t.Fatal(err)
	}
	if f.writeFlags&(1<<16) == 0 {
		t.Fatal("commit flag missing")
	}
	actual, err := client.ReadFile(ctx, "/cert/c2.der")
	if err != nil || !bytes.Equal(actual, payload) {
		t.Fatal("file round trip", err)
	}
	if _, err := client.ReadFile(ctx, "/missing"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	f.corruptRead = true
	if err := client.WriteFile(ctx, "/sys/mcuimg.bin", payload, WriteOptions{Verify: true}); err == nil {
		t.Fatal("failed to detect corrupt read-back")
	}
}
func TestClientChecksumFailureRequiresSync(t *testing.T) {
	f := newFake()
	f.corruptChecksum = true
	client := NewClient(f)
	ctx := context.Background()
	if _, err := client.Version(ctx); err == nil {
		t.Fatal("accepted bad checksum")
	}
	before := len(f.commands)
	if _, err := client.Version(ctx); !errors.Is(err, ErrReconnect) {
		t.Fatal(err)
	}
	if len(f.commands) != before {
		t.Fatal("sent commands after framing error")
	}
	f.corruptChecksum = false
	if err := client.Sync(ctx, false); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Version(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestClientConcurrentOperationAndCancellation(t *testing.T) {
	f := newFake()
	f.block = make(chan struct{})
	f.entered = make(chan struct{}, 1)
	client := NewClient(f)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := client.Version(ctx); done <- err }()
	<-f.entered
	if _, err := client.Version(context.Background()); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := client.Version(context.Background()); !errors.Is(err, ErrReconnect) {
		t.Fatal(err)
	}
}
func TestClientFlashWritePublishesHeaderLast(t *testing.T) {
	f := newFake()
	client := NewClient(f)
	payload := bytes.Repeat([]byte{0x5a}, len(f.flash))
	ctx := context.Background()
	if err := client.WriteFlash(ctx, payload, true, nil); err != nil {
		t.Fatal(err)
	}
	if len(f.writes) < 2 || f.writes[0] != 8 || f.writes[len(f.writes)-1] != 0 {
		t.Fatal("flash signature was not written last", f.writes)
	}
	actual, err := client.ReadFlash(ctx, 0, 0, nil)
	if err != nil || !bytes.Equal(actual, payload) {
		t.Fatal("flash roundtrip", err)
	}
	before := len(f.commands)
	if err := client.WriteFlash(ctx, payload[:len(payload)-BlockSize], false, nil); err == nil {
		t.Fatal("accepted size mismatch")
	}
	for _, op := range f.commands[before:] {
		if op == 0x30 || op == 0x2d {
			t.Fatal("invalid image mutated flash")
		}
	}
}
func TestClientRejectsInvalidWriteBeforeMutation(t *testing.T) {
	f := newFake()
	client := NewClient(f)
	for _, name := range []string{"../cert/private.der", "/cert/../private.der", "/cert/a\x00"} {
		if err := client.WriteFile(context.Background(), name, []byte{1}, WriteOptions{}); err == nil {
			t.Fatal(name)
		}
	}
	if err := client.WriteFile(context.Background(), "/cert/ca.der", []byte{1}, WriteOptions{Signature: []byte{1}}); err == nil {
		t.Fatal("accepted short signature")
	}
	if len(f.commands) != 0 {
		t.Fatal("invalid writes sent serial commands")
	}
}
func TestClientPrepareValidatesLoaderBeforeHandover(t *testing.T) {
	f := newFake()
	f.application = true
	client := NewClient(f)
	if err := client.Prepare(context.Background(), []byte("untrusted")); err == nil {
		t.Fatal("accepted wrong loader")
	}
	if !bytes.Equal(f.commands, []byte{0x2f}) {
		t.Fatal("handed over with wrong loader", f.commands)
	}
}
func TestClientPinnedLoaderHandover(t *testing.T) {
	name := os.Getenv("TEDDYCLOUD_CC3200_NWP_LOADER")
	if name == "" {
		t.Skip("external loader not supplied")
	}
	loader, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	f := newFake()
	f.application = true
	client := NewClient(f)
	if err = client.Prepare(context.Background(), loader); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.sram[:len(loader)], loader) {
		t.Fatal("loader upload differs")
	}
	if f.commands[len(f.commands)-1] != 0x32 {
		t.Fatal("loader was not executed")
	}
}
