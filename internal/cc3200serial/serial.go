// SPDX-License-Identifier: GPL-2.0-only
// Protocol reference: cc3200tool, Copyright (C) 2016-2020 Allterco Robotics
// and 2020- Team RevvoX (GPL-2.0-or-later).
// Package cc3200serial provides native UART and ESP32 UARTGWEX transports.
package cc3200serial

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
)

type Options struct {
	Gateway      bool
	Reset, SOP2  string
	Pins         [5]byte
	ResetWait    time.Duration
	ResetWaitSet bool
	PromptReset  func(context.Context, bool) error
	Debug        func(string)
}
type Port struct {
	port     serial.Port
	options  Options
	pending  []byte
	once     sync.Once
	done     chan struct{}
	closed   chan struct{}
	closeErr error
}

func Ports() ([]string, error) { return serial.GetPortsList() }
func (p *Port) debug(format string, args ...any) {
	if p.options.Debug != nil {
		p.options.Debug(fmt.Sprintf(format, args...))
	}
}
func Open(ctx context.Context, name string, options Options) (*Port, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for index, pin := range []string{options.Reset, options.SOP2} {
		if index == 0 && strings.TrimPrefix(pin, "~") == "prompt" && options.PromptReset != nil {
			continue
		}
		switch strings.TrimPrefix(pin, "~") {
		case "", "none", "dtr", "rts":
		default:
			return nil, fmt.Errorf("unsupported control line %q (use none, dtr, rts, ~dtr or ~rts)", pin)
		}
	}
	if a, b := strings.TrimPrefix(options.Reset, "~"), strings.TrimPrefix(options.SOP2, "~"); a != "" && a != "none" && a == b {
		return nil, errors.New("reset and SOP2 must use different modem lines")
	}
	p, err := serial.Open(name, &serial.Mode{BaudRate: 921600, DataBits: 8, Parity: serial.NoParity, StopBits: serial.OneStopBit, InitialStatusBits: &serial.ModemOutputBits{}})
	if err != nil {
		return nil, err
	}
	port := &Port{port: p, options: options, done: make(chan struct{})}
	go func() {
		select {
		case <-ctx.Done():
			_ = port.Close()
		case <-port.done:
		}
	}()
	if options.Gateway {
		if options.Pins == [5]byte{} {
			port.options.Pins = [5]byte{20, 21, 10, 9, 8}
		}
		if err = port.initGateway(ctx); err != nil {
			_ = port.Close()
			return nil, err
		}
	}
	return port, nil
}
func (p *Port) startClose() {
	p.once.Do(func() {
		close(p.done)
		p.closed = make(chan struct{})
		go func() {
			p.closeErr = p.port.Close()
			close(p.closed)
		}()
	})
}
func (p *Port) Close() error {
	p.startClose()
	// Some drivers wait for a stuck read/ioctl while closing. Cleanup must not
	// turn a bounded operation timeout into another indefinite wait.
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-p.closed:
		return p.closeErr
	case <-timer.C:
		return errors.New("serial driver did not finish closing within 250ms")
	}
}

// driverCall bounds calls even when a driver ignores its own read timeout.
// A normal read timeout leaves the connection available for synchronization
// retries. If the worker cannot return after cancellation, close the session
// rather than allow another operation to race a stuck OS call.
func (p *Port) driverCall(ctx context.Context, timeout time.Duration, stage string, work func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-p.done:
		return errors.New("serial connection closed; reconnect before retrying")
	default:
	}
	result := make(chan error, 1)
	go func() { result <- work(ctx) }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		// Reads poll at most every 50ms. Allow a short grace period for the
		// final poll to return, so an ordinary timeout is not a driver stall.
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-result:
			return fmt.Errorf("%s: %w", stage, ctx.Err())
		case <-timer.C:
		}
		p.startClose()
		return fmt.Errorf("%s: %w (serial connection closed; reconnect before retrying)", stage, ctx.Err())
	}
}
func (p *Port) readRaw(ctx context.Context, size int, timeout time.Duration) ([]byte, error) {
	var data []byte
	err := p.driverCall(ctx, timeout, "reading serial response", func(ctx context.Context) error {
		var err error
		data, err = p.readRawBlocking(ctx, size, timeout)
		return err
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}
func (p *Port) readRawBlocking(ctx context.Context, size int, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	data := make([]byte, size)
	for offset := 0; offset < size; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, errors.New("serial response timed out")
		}
		if err := p.port.SetReadTimeout(min(remaining, 50*time.Millisecond)); err != nil {
			return nil, err
		}
		n, err := p.port.Read(data[offset:])
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		offset += n
	}
	return data, nil
}
func (p *Port) writeRaw(ctx context.Context, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Closing the port bounds a stuck driver write and invalidates the session.
	watchdog := time.AfterFunc(5*time.Second, func() { _ = p.Close() })
	defer watchdog.Stop()
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := p.port.Write(data)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return ctx.Err()
}
func (p *Port) packet(ctx context.Context, kind uint16, data []byte) error {
	if len(data) > 65531 {
		return errors.New("gateway packet exceeds limit")
	}
	frame := make([]byte, 4+len(data))
	binary.LittleEndian.PutUint16(frame, uint16(len(frame)))
	binary.LittleEndian.PutUint16(frame[2:], kind)
	copy(frame[4:], data)
	return p.writeRaw(ctx, frame)
}
func (p *Port) frame(ctx context.Context, timeout time.Duration) (uint16, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	header, err := p.readRaw(ctx, 4, timeout)
	if err != nil {
		return 0, nil, err
	}
	// Firmware startup text can precede framed extended-mode data.
	for binary.LittleEndian.Uint16(header) < 4 || binary.LittleEndian.Uint16(header[2:]) > 10 {
		next, e := p.readRaw(ctx, 1, timeout)
		if e != nil {
			return 0, nil, e
		}
		copy(header, header[1:])
		header[3] = next[0]
	}
	size := int(binary.LittleEndian.Uint16(header)) - 4
	data, err := p.readRaw(ctx, size, timeout)
	kind := binary.LittleEndian.Uint16(header[2:])
	if err == nil {
		p.debug("Gateway RX type=%d payload=%d bytes", kind, len(data))
		if kind == 3 {
			p.debug("Gateway log: %q", data)
		}
	}
	return kind, data, err
}
func (p *Port) config(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		kind, data, err := p.frame(ctx, 5*time.Second)
		if err != nil {
			return nil, err
		}
		if kind == 1 {
			if len(data) != 12 {
				return nil, errors.New("invalid gateway config")
			}
			return data, nil
		}
	}
}
func (p *Port) initGateway(ctx context.Context) error {
	if err := p.port.ResetInputBuffer(); err != nil {
		return err
	}
	if err := p.packet(ctx, 10, []byte("UARTGWEX")); err != nil {
		return err
	}
	if err := delay(ctx, 300*time.Millisecond); err != nil {
		return err
	}
	if err := p.packet(ctx, 1, make([]byte, 12)); err != nil {
		return err
	}
	previous, err := p.config(ctx)
	if err != nil {
		return err
	}
	p.debug("Gateway current configuration: baud=%d TX%d RX%d reset%d control%d LED%d", binary.LittleEndian.Uint32(previous), previous[4], previous[5], previous[6], previous[7], previous[8])
	config := make([]byte, 12)
	binary.LittleEndian.PutUint32(config, 921600)
	copy(config[4:], p.options.Pins[:])
	config[11] = 1
	if err := p.packet(ctx, 1, config); err != nil {
		return err
	}
	actual, err := p.config(ctx)
	if err != nil {
		return err
	}
	if !bytes.Equal(actual[:9], config[:9]) {
		return errors.New("gateway rejected pins or baud rate")
	}
	p.debug("Gateway configuration verified: baud=%d TX%d RX%d reset%d control%d LED%d", binary.LittleEndian.Uint32(actual), actual[4], actual[5], actual[6], actual[7], actual[8])
	return nil
}
func (p *Port) Read(ctx context.Context, size int, timeout time.Duration) ([]byte, error) {
	if !p.options.Gateway {
		return p.readRaw(ctx, size, timeout)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for len(p.pending) < size {
		kind, data, err := p.frame(ctx, timeout)
		if err != nil {
			return nil, err
		}
		if kind == 0 {
			if len(p.pending)+len(data) > 1<<20 {
				return nil, errors.New("gateway buffer exceeds limit")
			}
			p.pending = append(p.pending, data...)
		}
	}
	result := append([]byte(nil), p.pending[:size]...)
	p.pending = p.pending[size:]
	return result, nil
}
func (p *Port) Write(ctx context.Context, data []byte) error {
	if p.options.Gateway {
		// Large USB bursts can overrun the gateway's receive path and leave
		// its packet parser waiting for missing bytes. Small DATA packets
		// preserve the target UART stream while allowing the gateway to drain.
		for len(data) > 0 {
			n := min(len(data), 64)
			if err := p.packet(ctx, 0, data[:n]); err != nil {
				return err
			}
			data = data[n:]
			if len(data) > 0 {
				if err := delay(ctx, 5*time.Millisecond); err != nil {
					return err
				}
			}
		}
		return ctx.Err()
	}
	return p.writeRaw(ctx, data)
}
func (p *Port) Flush(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.pending = nil
	return p.port.ResetInputBuffer()
}
func (p *Port) control(ctx context.Context, command string) error {
	p.debug("Gateway TX control %s", command)
	data := make([]byte, 16)
	copy(data, command)
	return p.packet(ctx, 2, data)
}
func (p *Port) Break(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.options.Gateway {
		if err := p.control(ctx, "B:200"); err != nil {
			return err
		}
		// The gateway starts BREAK asynchronously. An ACK can arrive while
		// TX is still held low; do not allow the next ROM command until the
		// requested break duration has elapsed, as with a native UART break.
		return delay(ctx, 200*time.Millisecond)
	}
	return p.driverCall(ctx, 1200*time.Millisecond, "sending UART break", func(context.Context) error {
		return p.port.Break(200 * time.Millisecond)
	})
}
func (p *Port) pin(pin string, level bool) error {
	if strings.HasPrefix(pin, "~") {
		level = !level
		pin = pin[1:]
	}
	switch pin {
	case "dtr":
		return p.port.SetDTR(level)
	case "rts":
		return p.port.SetRTS(level)
	default:
		return nil
	}
}
func (p *Port) Reset(ctx context.Context, boot bool) error {
	if p.options.Gateway {
		command := "C:0"
		if boot {
			command = "C:1"
		}
		if err := p.control(ctx, command); err != nil {
			return err
		}
		if err := p.control(ctx, "R:0"); err != nil {
			return err
		}
		if err := delay(ctx, 100*time.Millisecond); err != nil {
			return err
		}
		if err := p.control(ctx, "R:1"); err != nil {
			return err
		}
	} else {
		if err := p.pin(p.options.SOP2, boot); err != nil {
			return err
		}
		if strings.TrimPrefix(p.options.Reset, "~") == "prompt" {
			if err := p.options.PromptReset(ctx, boot); err != nil {
				return err
			}
		} else {
			if err := p.pin(p.options.Reset, true); err != nil {
				return err
			}
			if err := delay(ctx, 100*time.Millisecond); err != nil {
				return err
			}
			if err := p.pin(p.options.Reset, false); err != nil {
				return err
			}
		}
	}
	wait := p.options.ResetWait
	if !p.options.ResetWaitSet {
		wait = time.Second
	}
	return delay(ctx, wait)
}
func delay(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
