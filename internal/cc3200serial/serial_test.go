// SPDX-License-Identifier: GPL-2.0-only
package cc3200serial

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"testing"
	"time"

	"go.bug.st/serial"
)

type mockPort struct {
	input, output []byte
	fragment      int
}

func (m *mockPort) Read(data []byte) (int, error) {
	if len(m.input) == 0 {
		return 0, io.EOF
	}
	n := min(len(data), len(m.input), m.fragment)
	copy(data, m.input[:n])
	m.input = m.input[n:]
	return n, nil
}
func (m *mockPort) Write(data []byte) (int, error) {
	n := min(len(data), m.fragment)
	m.output = append(m.output, data[:n]...)
	return n, nil
}
func (*mockPort) SetMode(*serial.Mode) error { return nil }
func (*mockPort) Drain() error               { return nil }
func (*mockPort) ResetInputBuffer() error    { return nil }
func (*mockPort) ResetOutputBuffer() error   { return nil }
func (*mockPort) SetDTR(bool) error          { return nil }
func (*mockPort) SetRTS(bool) error          { return nil }
func (*mockPort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	return &serial.ModemStatusBits{}, nil
}
func (*mockPort) SetReadTimeout(time.Duration) error { return nil }
func (*mockPort) Close() error                       { return nil }
func (*mockPort) Break(time.Duration) error          { return nil }
func gatewayFrame(kind uint16, data []byte) []byte {
	frame := make([]byte, 4+len(data))
	binary.LittleEndian.PutUint16(frame, uint16(len(frame)))
	binary.LittleEndian.PutUint16(frame[2:], kind)
	copy(frame[4:], data)
	return frame
}
func TestNativeGatewayFragmentedFramesAndWrites(t *testing.T) {
	mock := &mockPort{fragment: 1}
	mock.input = append(gatewayFrame(3, []byte("log")), gatewayFrame(0, []byte{0, 0xcc, 1, 2, 3})...)
	port := &Port{port: mock, options: Options{Gateway: true}, done: make(chan struct{})}
	ctx := context.Background()
	data, err := port.Read(ctx, 2, time.Second)
	if err != nil || !bytes.Equal(data, []byte{0, 0xcc}) {
		t.Fatal(data, err)
	}
	data, err = port.Read(ctx, 3, time.Second)
	if err != nil || !bytes.Equal(data, []byte{1, 2, 3}) {
		t.Fatal(data, err)
	}
	if err = port.Write(ctx, []byte{9, 8, 7}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mock.output, gatewayFrame(0, []byte{9, 8, 7})) {
		t.Fatal(mock.output)
	}
}
func TestNativeSerialCancellationAndIncompleteFrames(t *testing.T) {
	mock := &mockPort{fragment: 1, input: []byte{1}}
	port := &Port{port: mock, done: make(chan struct{})}
	if _, err := port.Read(context.Background(), 2, time.Second); err == nil {
		t.Fatal("accepted short serial response")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := port.Write(ctx, []byte{1}); err == nil {
		t.Fatal("wrote after cancellation")
	}
	if len(mock.output) != 0 {
		t.Fatal("serial data after cancellation")
	}
}
