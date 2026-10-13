// SPDX-License-Identifier: GPL-2.0-only
// Protocol reference: cc3200tool, Copyright (C) 2016-2020 Allterco Robotics
// and 2020- Team RevvoX (GPL-2.0-or-later).
package cc3200

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

// Link owns timeouts and cancellation for a single serial connection. It must
// return exactly n bytes or an error; a transport failure requires a new sync.
type Link interface {
	Read(context.Context, int, time.Duration) ([]byte, error)
	Write(context.Context, []byte) error
	Flush(context.Context) error
	Break(context.Context) error
	Reset(context.Context, bool) error
}

type Client struct {
	link         Link
	log          func(string)
	debug        func(string)
	eraseTimeout time.Duration
	loaderSHA256 string
	busy         atomic.Bool
	broken       bool // accessed only while busy is held
}

func NewClient(link Link) *Client { return NewClientWithOptions(link, ClientOptions{}) }

// ClientOptions configures diagnostics, erase timing and loader selection. Callbacks run
// synchronously and must not call back into the client. No payload bytes are logged.
type ClientOptions struct {
	Status, Debug func(string)
	EraseTimeout  time.Duration
	// LoaderSHA256 selects an explicitly supplied loader. Empty uses the
	// pinned stock loader; browser callers retain that default.
	LoaderSHA256 string
}

func NewClientWithOptions(link Link, options ClientOptions) *Client {
	if options.EraseTimeout <= 0 {
		options.EraseTimeout = eraseTimeout
	}
	if options.LoaderSHA256 == "" {
		options.LoaderSHA256 = LoaderSHA256
	}
	return &Client{link: link, log: options.Status, debug: options.Debug, eraseTimeout: options.EraseTimeout, loaderSHA256: options.LoaderSHA256}
}

func (c *Client) report(format string, args ...any) {
	if c.log != nil {
		c.log(fmt.Sprintf(format, args...))
	}
}

func (c *Client) progress(label string) Progress {
	var last time.Time
	return func(done, total int) {
		if c.log == nil {
			return
		}
		if done != total && time.Since(last) < time.Second {
			return
		}
		last = time.Now()
		percent := 100
		if total > 0 {
			percent = done * 100 / total
		}
		c.report("%s: %d/%d bytes (%d%%)", label, done, total, percent)
	}
}

var ErrBusy = errors.New("CC3200 operation already in progress")
var ErrReconnect = errors.New("CC3200 serial state lost; reconnect before retrying")

const LoaderSHA256 = "2c9a05c469389446e05871a88e84ca3994bf3d6a6785155135db21d33aa63cc9"
const commandTimeout = 5 * time.Second
const eraseTimeout = 120 * time.Second
const SerialFlash uint32 = 2
const SRAM uint32 = 0

type Version struct{ Bootloader, NWP, MAC, PHY, Chip [4]byte }
type StorageInfo struct{ BlockSize, Blocks uint16 }

func (s StorageInfo) Size() int { return int(s.BlockSize) * int(s.Blocks) }

type FileInfo struct {
	Exists bool
	Size   int
}
type WriteOptions struct {
	Allocation int
	Commit     bool
	Signature  []byte
	Verify     bool
}
type Progress func(done, total int)

func (c *Client) run(ctx context.Context, recovery bool, work func() error) error {
	if !c.busy.CompareAndSwap(false, true) {
		return ErrBusy
	}
	defer c.busy.Store(false)
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.broken && !recovery {
		return ErrReconnect
	}
	return work()
}
func words(op byte, values ...uint32) []byte {
	data := make([]byte, 1+4*len(values))
	data[0] = op
	for i, v := range values {
		binary.BigEndian.PutUint32(data[1+4*i:], v)
	}
	return data
}
func checksum(data []byte) byte {
	var sum byte
	for _, b := range data {
		sum += b
	}
	return sum
}
func (c *Client) read(ctx context.Context, n int, timeout time.Duration) ([]byte, error) {
	data, err := c.link.Read(ctx, n, timeout)
	if err == nil && len(data) != n {
		err = errors.New("short serial response")
	}
	if err != nil {
		c.broken = true
	}
	return data, err
}
func (c *Client) write(ctx context.Context, data []byte) error {
	if err := c.link.Write(ctx, data); err != nil {
		c.broken = true
		return err
	}
	return nil
}
func (c *Client) ack(ctx context.Context, timeout time.Duration) error {
	// Like the Python tool, scan for the two-byte ACK: startup text and
	// break-related bytes can precede it. One deadline bounds the whole scan,
	// even if an application keeps sending output instead of an ACK.
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	deadline, _ := ctx.Deadline()
	var previous byte
	for received := 0; ; received++ {
		data, err := c.read(ctx, 1, time.Until(deadline))
		if err != nil {
			return fmt.Errorf("waiting for CC3200 ACK (00 CC); received %d bytes without a complete ACK: %w", received, err)
		}
		if received > 0 && previous == 0 && data[0] == 0xcc {
			if c.debug != nil && received > 1 {
				c.debug(fmt.Sprintf("Skipped %d bytes before CC3200 ACK", received-1))
			}
			return nil
		}
		previous = data[0]
	}
}

// response=0: ACK only; response=-1: framed response; response>0: raw bytes.
func (c *Client) command(ctx context.Context, payload []byte, response int, timeout time.Duration) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(payload) == 0 || len(payload) > 65533 {
		return nil, errors.New("invalid command size")
	}
	if c.debug != nil {
		c.debug(fmt.Sprintf("TX opcode=0x%02x payload=%d bytes response=%d timeout=%s", payload[0], len(payload), response, timeout))
	}
	frame := make([]byte, 3+len(payload))
	binary.BigEndian.PutUint16(frame, uint16(len(payload)+2))
	frame[2] = checksum(payload)
	copy(frame[3:], payload)
	if err := c.write(ctx, frame); err != nil {
		return nil, err
	}
	if err := c.ack(ctx, timeout); err != nil {
		return nil, fmt.Errorf("opcode 0x%02x waiting for ACK: %w", payload[0], err)
	}
	if response == 0 {
		return nil, nil
	}
	if response > 0 {
		return c.read(ctx, response, timeout)
	}
	header, err := c.read(ctx, 3, timeout)
	if err != nil {
		return nil, err
	}
	size := int(binary.BigEndian.Uint16(header)) - 2
	if size < 0 {
		c.broken = true
		return nil, errors.New("invalid CC3200 response length")
	}
	data, err := c.read(ctx, size, timeout)
	if err != nil {
		return nil, err
	}
	if checksum(data) != header[2] {
		c.broken = true
		return nil, errors.New("CC3200 response checksum mismatch")
	}
	if err := c.write(ctx, []byte{0, 0xcc}); err != nil {
		return nil, err
	}
	return data, nil
}
func pause(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func (c *Client) sync(ctx context.Context, reset bool) error {
	if err := c.link.Flush(ctx); err != nil {
		c.broken = true
		return err
	}
	if reset {
		c.report("Resetting device into bootloader mode")
		if err := c.link.Reset(ctx, true); err != nil {
			c.broken = true
			return err
		}
	}
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if err := ctx.Err(); err != nil {
			c.broken = true
			return err
		}
		c.report("Synchronizing with bootloader (attempt %d/5, ACK timeout 1s)", attempt+1)
		if c.debug != nil {
			c.debug("Sending UART break (200ms)")
		}
		if err := c.link.Break(ctx); err != nil {
			c.broken = true
			return err
		}
		if c.debug != nil {
			c.debug("UART break sent; waiting for bootloader ACK (00 CC)")
		}
		if err := c.ack(ctx, time.Second); err == nil {
			c.broken = false
			c.report("Bootloader synchronized")
			return nil
		} else {
			lastErr = err
			if ctx.Err() != nil {
				return ctx.Err()
			}
			c.report("Synchronization attempt %d failed: %v", attempt+1, err)
		}
	}
	return fmt.Errorf("CC3200 sync failed (check SOP2/bootloader mode, reset and UART wiring): %w: %w", ErrReconnect, lastErr)
}
func (c *Client) Sync(ctx context.Context, reset bool) error {
	return c.run(ctx, true, func() error { return c.sync(ctx, reset) })
}
func (c *Client) version(ctx context.Context) (Version, error) {
	var v Version
	data, err := c.command(ctx, []byte{0x2f}, -1, commandTimeout)
	if err != nil {
		return v, err
	}
	if len(data) != 28 {
		c.broken = true
		return v, errors.New("invalid version response")
	}
	copy(v.Bootloader[:], data[:4])
	copy(v.NWP[:], data[4:8])
	copy(v.MAC[:], data[8:12])
	copy(v.PHY[:], data[12:16])
	copy(v.Chip[:], data[16:20])
	return v, nil
}
func (c *Client) Version(ctx context.Context) (v Version, err error) {
	err = c.run(ctx, false, func() error { v, err = c.version(ctx); return err })
	return
}
func (c *Client) Prepare(ctx context.Context, loader []byte) error {
	return c.run(ctx, true, func() error {
		if err := c.sync(ctx, true); err != nil {
			return err
		}
		c.report("Querying ROM/NWP version")
		version, err := c.version(ctx)
		if err != nil {
			return err
		}
		c.report("Bootloader %v; NWP %v; chip %v", version.Bootloader, version.NWP, version.Chip)
		if version.Chip[0]&0x10 == 0 {
			c.report("NWP session already active; no loader needed")
			return nil
		} // NWP is already active.
		if version.Bootloader[1] < 4 {
			return errors.New("CC3200 application ROM revisions below 4 are unsupported")
		}
		c.report("Application ROM active; validating rbtl3100s.dll loader")
		if len(loader) == 0 || len(loader) > 128<<10 || Digest(loader) != c.loaderSHA256 {
			return errors.New("NWP loader must match the expected SHA-256 and contain 1..131072 bytes")
		}
		c.report("Switching ROM clock; waiting 1.2s for handover")
		if _, err = c.command(ctx, words(0x33, 26666667), 0, commandTimeout); err != nil {
			return err
		}
		if err = pause(ctx, 1200*time.Millisecond); err != nil {
			c.broken = true
			return err
		}
		if err = c.sync(ctx, false); err != nil {
			return err
		}
		c.report("Uploading NWP loader to SRAM (%d bytes)", len(loader))
		if err = c.rawWrite(ctx, SRAM, 0, loader, nil); err != nil {
			return err
		}
		c.report("Starting NWP loader; waiting for ACK (timeout 5s)")
		if _, err = c.command(ctx, []byte{0x32}, 0, commandTimeout); err != nil {
			return err
		}
		if err = c.ack(ctx, commandTimeout); err != nil {
			return err
		}
		c.report("NWP loader ready")
		return nil
	})
}
func (c *Client) Reboot(ctx context.Context) error {
	return c.run(ctx, true, func() error {
		c.report("Rebooting device into application mode")
		c.broken = true
		return c.link.Reset(ctx, false)
	})
}
func (c *Client) storageInfo(ctx context.Context, storage uint32) (StorageInfo, error) {
	c.report("Querying storage geometry")
	var info StorageInfo
	data, err := c.command(ctx, words(0x31, storage), -1, commandTimeout)
	if err != nil {
		return info, err
	}
	if len(data) < 4 {
		c.broken = true
		return info, errors.New("invalid storage information")
	}
	info = StorageInfo{binary.BigEndian.Uint16(data), binary.BigEndian.Uint16(data[2:])}
	if info.BlockSize == 0 || info.Blocks == 0 || info.Size() > MaxFlash {
		return StorageInfo{}, errors.New("unsupported storage geometry")
	}
	c.report("Storage: %d bytes (%d blocks of %d bytes)", info.Size(), info.Blocks, info.BlockSize)
	return info, nil
}
func (c *Client) StorageInfo(ctx context.Context, storage uint32) (info StorageInfo, err error) {
	err = c.run(ctx, false, func() error { info, err = c.storageInfo(ctx, storage); return err })
	return
}
func (c *Client) StorageList(ctx context.Context) (value byte, err error) {
	err = c.run(ctx, false, func() error {
		data, e := c.command(ctx, []byte{0x27}, 1, commandTimeout)
		if e == nil {
			value = data[0]
		}
		return e
	})
	return
}
func (c *Client) rawRead(ctx context.Context, storage uint32, offset, size int, progress Progress) ([]byte, error) {
	if offset < 0 || size < 0 || offset > MaxFlash || size > MaxFlash-offset {
		return nil, errors.New("invalid raw read bounds")
	}
	data := make([]byte, size)
	c.report("Reading storage %d at offset 0x%x (%d bytes)", storage, offset, size)
	report := c.progress("Read storage")
	for pos := 0; pos < size; pos += 4096 {
		count := min(4096, size-pos)
		chunk, err := c.command(ctx, words(0x2c, storage, uint32(offset+pos), uint32(count)), -1, commandTimeout)
		if err != nil {
			return nil, err
		}
		if len(chunk) != count {
			c.broken = true
			return nil, errors.New("short storage response")
		}
		copy(data[pos:], chunk)
		report(pos+count, size)
		if progress != nil {
			progress(pos+count, size)
		}
	}
	return data, nil
}
func (c *Client) rawWrite(ctx context.Context, storage uint32, offset int, data []byte, progress Progress) error {
	if offset < 0 || offset > MaxFlash || len(data) > MaxFlash-offset {
		return errors.New("invalid raw write bounds")
	}
	c.report("Writing storage %d at offset 0x%x (%d bytes)", storage, offset, len(data))
	report := c.progress("Write storage")
	for pos := 0; pos < len(data); pos += 4080 {
		chunk := data[pos:min(pos+4080, len(data))]
		payload := append(words(0x2d, storage, uint32(offset+pos), uint32(len(chunk))), chunk...)
		if _, err := c.command(ctx, payload, 0, commandTimeout); err != nil {
			return err
		}
		report(pos+len(chunk), len(data))
		if progress != nil {
			progress(pos+len(chunk), len(data))
		}
	}
	return nil
}
func (c *Client) ReadFlash(ctx context.Context, offset, size int, progress Progress) (data []byte, err error) {
	return c.ReadFlashWithOptions(ctx, offset, size, false, progress)
}

// ReadFlashWithOptions optionally bypasses reported device capacity, while
// retaining the absolute 16MiB protocol bound.
func (c *Client) ReadFlashWithOptions(ctx context.Context, offset, size int, ignoreMax bool, progress Progress) (data []byte, err error) {
	err = c.run(ctx, false, func() error {
		info, e := c.storageInfo(ctx, SerialFlash)
		if e != nil {
			return e
		}
		if info.BlockSize != BlockSize || offset < 0 || (!ignoreMax && offset > info.Size()) {
			return errors.New("unsupported flash geometry or offset")
		}
		if size == 0 {
			size = info.Size() - offset
		}
		if size < 0 || (!ignoreMax && size > info.Size()-offset) {
			return errors.New("read exceeds flash")
		}
		data, e = c.rawRead(ctx, SerialFlash, offset, size, progress)
		return e
	})
	return
}
func (c *Client) WriteFlash(ctx context.Context, data []byte, verify bool, progress Progress) error {
	return c.WriteFlashWithOptions(ctx, data, verify, false, progress)
}

func (c *Client) WriteFlashWithOptions(ctx context.Context, data []byte, verify, noErase bool, progress Progress) error {
	if len(data) < 5*BlockSize || len(data) > MaxFlash || len(data)%BlockSize != 0 {
		return errors.New("invalid flash image size")
	}
	return c.run(ctx, false, func() error {
		info, err := c.storageInfo(ctx, SerialFlash)
		if err != nil {
			return err
		}
		if info.BlockSize != BlockSize || len(data) != info.Size() {
			return errors.New("flash image must match the full device size")
		}
		if !noErase {
			c.report("Erasing full flash (timeout %s)", c.eraseTimeout)
			if _, err = c.command(ctx, words(0x30, SerialFlash, 0, uint32(info.Blocks)), 0, c.eraseTimeout); err != nil {
				return err
			}
		}
		if err = c.rawWrite(ctx, SerialFlash, 8, data[8:], progress); err != nil {
			return err
		}
		// Publish the filesystem signature only after the remaining image is written.
		if err = c.rawWrite(ctx, SerialFlash, 0, data[:8], nil); err != nil {
			return err
		}
		if verify {
			c.report("Verifying flash by reading it back")
			actual, e := c.rawRead(ctx, SerialFlash, 0, len(data), nil)
			if e != nil {
				return e
			}
			if !bytes.Equal(actual, data) {
				return errors.New("flash read-back verification failed")
			}
			c.report("Flash read-back verified")
		}
		return nil
	})
}
