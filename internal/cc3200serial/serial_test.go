// SPDX-License-Identifier: GPL-2.0-only
package cc3200serial

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"sync"
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

func TestGatewayLargeWritePreservesUARTStream(t *testing.T) {
	mock := &mockPort{fragment: 7}
	port := &Port{port: mock, options: Options{Gateway: true}, done: make(chan struct{})}
	data := make([]byte, 193)
	for i := range data {
		data[i] = byte(i*73 + 19)
	}
	if err := port.Write(context.Background(), data); err != nil {
		t.Fatal(err)
	}
	var stream []byte
	frames := 0
	for remaining := mock.output; len(remaining) > 0; frames++ {
		if len(remaining) < 4 {
			t.Fatal("incomplete gateway header")
		}
		size := int(binary.LittleEndian.Uint16(remaining))
		if size < 5 || size > 68 || size > len(remaining) || binary.LittleEndian.Uint16(remaining[2:]) != 0 {
			t.Fatal("invalid DATA fragment", size)
		}
		stream = append(stream, remaining[4:size]...)
		remaining = remaining[size:]
	}
	if frames != 4 || !bytes.Equal(stream, data) {
		t.Fatal("fragmentation changed the UART byte stream", frames)
	}
}

type cancelWritePort struct {
	mockPort
	cancel context.CancelFunc
}

func (p *cancelWritePort) Write(data []byte) (int, error) {
	n, err := p.mockPort.Write(data)
	p.cancel()
	return n, err
}

func TestGatewayPacedWriteStopsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mock := &cancelWritePort{mockPort: mockPort{fragment: 4096}, cancel: cancel}
	port := &Port{port: mock, options: Options{Gateway: true}, done: make(chan struct{})}
	data := make([]byte, 128)
	if err := port.Write(ctx, data); !errors.Is(err, context.Canceled) {
		t.Fatal("paced write did not honor cancellation", err)
	}
	if !bytes.Equal(mock.output, gatewayFrame(0, data[:64])) {
		t.Fatal("sent another fragment after cancellation")
	}
}

func TestGatewayBreakWaitsBeforeAllowingNextCommand(t *testing.T) {
	for _, cancelEarly := range []bool{false, true} {
		mock := &mockPort{fragment: 1}
		port := &Port{port: mock, options: Options{Gateway: true}, done: make(chan struct{})}
		ctx := context.Background()
		cancel := func() {}
		if cancelEarly {
			ctx, cancel = context.WithTimeout(ctx, 20*time.Millisecond)
		}
		start := time.Now()
		err := port.Break(ctx)
		cancel()
		elapsed := time.Since(start)
		if cancelEarly {
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("break did not honor cancellation", err)
			}
		} else if err != nil || elapsed < 200*time.Millisecond {
			t.Fatal("returned before gateway finished break", elapsed, err)
		}
		payload := make([]byte, 16)
		copy(payload, "B:200")
		if !bytes.Equal(mock.output, gatewayFrame(2, payload)) {
			t.Fatal("incorrect break command", mock.output)
		}
	}
}

// Simulate a driver that ignores read timeouts and cannot close until its
// outstanding OS operation returns.
type stalledPort struct {
	mockPort
	entered, release chan struct{}
	once             sync.Once
}

func (p *stalledPort) stall() error {
	p.once.Do(func() { close(p.entered) })
	<-p.release
	return io.EOF
}
func (p *stalledPort) Read([]byte) (int, error)  { return 0, p.stall() }
func (p *stalledPort) Break(time.Duration) error { return p.stall() }
func (p *stalledPort) Close() error              { <-p.release; return nil }

func TestDriverStallsCannotBlockTimeoutOrCleanup(t *testing.T) {
	for _, operation := range []string{"read", "break"} {
		t.Run(operation, func(t *testing.T) {
			mock := &stalledPort{entered: make(chan struct{}), release: make(chan struct{})}
			defer close(mock.release)
			port := &Port{port: mock, done: make(chan struct{})}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			start := time.Now()
			var err error
			if operation == "read" {
				_, err = port.Read(ctx, 1, time.Second)
			} else {
				err = port.Break(ctx)
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			select {
			case <-mock.entered:
			default:
				t.Fatal("driver operation never started")
			}
			if err := port.Close(); err == nil {
				t.Fatal("stuck close did not report timeout")
			}
			if time.Since(start) > time.Second {
				t.Fatal("driver stall prevented bounded shutdown")
			}
			if err := port.Break(context.Background()); err == nil {
				t.Fatal("reused timed-out connection")
			}
		})
	}
}

func TestDriverReadStallHonorsCancellation(t *testing.T) {
	mock := &stalledPort{entered: make(chan struct{}), release: make(chan struct{})}
	defer close(mock.release)
	port := &Port{port: mock, done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := port.Read(ctx, 1, time.Second); result <- err }()
	<-mock.entered
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled read remained blocked in driver")
	}
}

type pollingPort struct{ mockPort }

func (*pollingPort) Read([]byte) (int, error) {
	time.Sleep(10 * time.Millisecond)
	return 0, nil
}

func TestNormalReadTimeoutAllowsSynchronizationRetry(t *testing.T) {
	port := &Port{port: &pollingPort{}, done: make(chan struct{})}
	defer port.Close()
	for attempt := 0; attempt < 5; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
		_, err := port.Read(ctx, 1, time.Second)
		cancel()
		if err == nil {
			t.Fatal("silent UART did not time out")
		}
		select {
		case <-port.done:
			t.Fatal("ordinary ACK timeout closed connection")
		default:
		}
		if err := port.Break(context.Background()); err != nil {
			t.Fatal("could not retry synchronization", err)
		}
	}
}
