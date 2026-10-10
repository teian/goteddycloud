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
	link   Link
	busy   atomic.Bool
	broken bool // accessed only while busy is held
}

func NewClient(link Link) *Client { return &Client{link: link} }

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
	data, err := c.read(ctx, 2, timeout)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, []byte{0, 0xcc}) {
		c.broken = true
		return errors.New("invalid CC3200 ACK")
	}
	return nil
}

// response=0: ACK only; response=-1: framed response; response>0: raw bytes.
func (c *Client) command(ctx context.Context, payload []byte, response int, timeout time.Duration) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(payload) == 0 || len(payload) > 65533 {
		return nil, errors.New("invalid command size")
	}
	frame := make([]byte, 3+len(payload))
	binary.BigEndian.PutUint16(frame, uint16(len(payload)+2))
	frame[2] = checksum(payload)
	copy(frame[3:], payload)
	if err := c.write(ctx, frame); err != nil {
		return nil, err
	}
	if err := c.ack(ctx, timeout); err != nil {
		return nil, err
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
		if err := c.link.Reset(ctx, true); err != nil {
			c.broken = true
			return err
		}
	}
	for attempt := 0; attempt < 5; attempt++ {
		if err := ctx.Err(); err != nil {
			c.broken = true
			return err
		}
		if err := c.link.Break(ctx); err != nil {
			c.broken = true
			return err
		}
		if err := c.ack(ctx, 700*time.Millisecond); err == nil {
			c.broken = false
			return nil
		}
	}
	return fmt.Errorf("CC3200 sync failed: %w", ErrReconnect)
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
		version, err := c.version(ctx)
		if err != nil {
			return err
		}
		if version.Chip[0]&0x10 == 0 {
			return nil
		} // NWP is already active.
		if version.Bootloader[1] < 4 {
			return errors.New("CC3200 application ROM revisions below 4 are unsupported")
		}
		if len(loader) > 128<<10 || Digest(loader) != LoaderSHA256 {
			return errors.New("select the pinned cc3200tool rbtl3100s.dll loader")
		}
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
		if err = c.rawWrite(ctx, SRAM, 0, loader, nil); err != nil {
			return err
		}
		if _, err = c.command(ctx, []byte{0x32}, 0, commandTimeout); err != nil {
			return err
		}
		return c.ack(ctx, commandTimeout)
	})
}
func (c *Client) Reboot(ctx context.Context) error {
	return c.run(ctx, true, func() error { c.broken = true; return c.link.Reset(ctx, false) })
}
func (c *Client) storageInfo(ctx context.Context, storage uint32) (StorageInfo, error) {
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
	for pos := 0; pos < len(data); pos += 4080 {
		chunk := data[pos:min(pos+4080, len(data))]
		payload := append(words(0x2d, storage, uint32(offset+pos), uint32(len(chunk))), chunk...)
		if _, err := c.command(ctx, payload, 0, commandTimeout); err != nil {
			return err
		}
		if progress != nil {
			progress(pos+len(chunk), len(data))
		}
	}
	return nil
}
func (c *Client) ReadFlash(ctx context.Context, offset, size int, progress Progress) (data []byte, err error) {
	err = c.run(ctx, false, func() error {
		info, e := c.storageInfo(ctx, SerialFlash)
		if e != nil {
			return e
		}
		if info.BlockSize != BlockSize || offset < 0 || offset > info.Size() {
			return errors.New("unsupported flash geometry or offset")
		}
		if size == 0 {
			size = info.Size() - offset
		}
		if size < 0 || size > info.Size()-offset {
			return errors.New("read exceeds flash")
		}
		data, e = c.rawRead(ctx, SerialFlash, offset, size, progress)
		return e
	})
	return
}
func (c *Client) WriteFlash(ctx context.Context, data []byte, verify bool, progress Progress) error {
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
		if _, err = c.command(ctx, words(0x30, SerialFlash, 0, uint32(info.Blocks)), 0, eraseTimeout); err != nil {
			return err
		}
		if err = c.rawWrite(ctx, SerialFlash, 8, data[8:], progress); err != nil {
			return err
		}
		// Publish the filesystem signature only after the remaining image is written.
		if err = c.rawWrite(ctx, SerialFlash, 0, data[:8], nil); err != nil {
			return err
		}
		if verify {
			actual, e := c.rawRead(ctx, SerialFlash, 0, len(data), nil)
			if e != nil {
				return e
			}
			if !bytes.Equal(actual, data) {
				return errors.New("flash read-back verification failed")
			}
		}
		return nil
	})
}
