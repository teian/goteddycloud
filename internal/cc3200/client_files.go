// SPDX-License-Identifier: GPL-2.0-only
package cc3200

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

func (c *Client) status(ctx context.Context) error {
	data, err := c.command(ctx, []byte{0x23}, -1, commandTimeout)
	if err != nil {
		return err
	}
	if len(data) != 1 || data[0] != 0x40 {
		return fmt.Errorf("CC3200 file operation failed (status %x)", data)
	}
	return nil
}
func (c *Client) fileInfo(ctx context.Context, name string) (FileInfo, error) {
	var info FileInfo
	if !ValidName(name) {
		return info, errors.New("invalid device filename")
	}
	data, err := c.command(ctx, append(words(0x2a, uint32(len(name))), []byte(name)...), -1, commandTimeout)
	if err != nil {
		return info, err
	}
	if len(data) < 8 || data[0] > 1 {
		c.broken = true
		return info, errors.New("invalid file information")
	}
	info = FileInfo{data[0] == 1, int(binary.BigEndian.Uint32(data[4:8]))}
	if info.Size > MaxFlash {
		return FileInfo{}, errors.New("device file exceeds limit")
	}
	return info, nil
}
func (c *Client) FileInfo(ctx context.Context, name string) (info FileInfo, err error) {
	err = c.run(ctx, false, func() error { info, err = c.fileInfo(ctx, name); return err })
	return
}
func (c *Client) openFile(ctx context.Context, name string, flags uint32) error {
	payload := append(words(0x21, flags, 0), []byte(name)...)
	payload = append(payload, 0, 0)
	_, err := c.command(ctx, payload, 4, eraseTimeout)
	return err
}
func (c *Client) closeFile(ctx context.Context, signature []byte) error {
	if signature == nil {
		signature = bytes.Repeat([]byte{0x46}, 256)
	}
	payload := make([]byte, 321)
	payload[0] = 0x22
	copy(payload[64:], signature)
	if _, err := c.command(ctx, payload, 0, commandTimeout); err != nil {
		return err
	}
	return c.status(ctx)
}
func (c *Client) readFile(ctx context.Context, name string) ([]byte, error) {
	info, err := c.fileInfo(ctx, name)
	if err != nil {
		return nil, err
	}
	if !info.Exists {
		return nil, os.ErrNotExist
	}
	if err = c.openFile(ctx, name, 0); err != nil {
		return nil, err
	}
	data := make([]byte, info.Size)
	for pos := 0; pos < len(data); pos += 4096 {
		count := min(4096, len(data)-pos)
		chunk, e := c.command(ctx, words(0x2b, uint32(pos), uint32(count)), -1, commandTimeout)
		if e != nil {
			return nil, e
		}
		if len(chunk) != count {
			c.broken = true
			return nil, errors.New("short file response")
		}
		copy(data[pos:], chunk)
	}
	if err = c.closeFile(ctx, nil); err != nil {
		return nil, err
	}
	return data, nil
}
func (c *Client) ReadFile(ctx context.Context, name string) (data []byte, err error) {
	err = c.run(ctx, false, func() error { data, err = c.readFile(ctx, name); return err })
	return
}
func (c *Client) eraseFile(ctx context.Context, name string) error {
	if !ValidName(name) {
		return errors.New("invalid device filename")
	}
	payload := append(words(0x2e, 0), []byte(name)...)
	payload = append(payload, 0)
	if _, err := c.command(ctx, payload, 0, eraseTimeout); err != nil {
		return err
	}
	return c.status(ctx)
}
func (c *Client) EraseFile(ctx context.Context, name string) error {
	return c.run(ctx, false, func() error { return c.eraseFile(ctx, name) })
}
func (c *Client) WriteFile(ctx context.Context, name string, data []byte, options WriteOptions) error {
	if !ValidName(name) || len(data) == 0 || len(data) > MaxFlash || options.Allocation < 0 || options.Allocation > MaxFlash {
		return errors.New("invalid file or allocation")
	}
	if options.Signature != nil && len(options.Signature) != 256 {
		return errors.New("signature must contain exactly 256 bytes")
	}
	allocation := max(options.Allocation, len(data))
	sizes := []int{256, 1024, 4096, 16384, 65536}
	index := 0
	for index < len(sizes) && sizes[index]*255 < allocation {
		index++
	}
	if index == len(sizes) {
		return errors.New("file allocation too large")
	}
	flags := uint32(0x3000 | index<<8 | (allocation+sizes[index]-1)/sizes[index])
	if options.Commit {
		flags |= 1 << 16
	}
	if options.Signature != nil {
		flags |= 0x23 << 16
	}
	return c.run(ctx, false, func() error {
		info, err := c.fileInfo(ctx, name)
		if err != nil {
			return err
		}
		if info.Exists {
			if err = c.eraseFile(ctx, name); err != nil {
				return err
			}
		}
		if err = c.openFile(ctx, name, flags); err != nil {
			return err
		}
		for pos := 0; pos < len(data); pos += 4096 {
			payload := append(words(0x24, uint32(pos)), data[pos:min(pos+4096, len(data))]...)
			if _, err = c.command(ctx, payload, 0, commandTimeout); err != nil {
				return err
			}
			if err = c.status(ctx); err != nil {
				return err
			}
		}
		if err = c.closeFile(ctx, options.Signature); err != nil {
			return err
		}
		if options.Verify {
			actual, e := c.readFile(ctx, name)
			if e != nil {
				return e
			}
			if !bytes.Equal(actual, data) {
				return errors.New("file read-back verification failed")
			}
		}
		return nil
	})
}
func (c *Client) Format(ctx context.Context, size int) error {
	if size < 512<<10 || size > MaxFlash || size&(size-1) != 0 {
		return errors.New("flash size must be 512KiB, 1, 2, 4, 8 or 16MiB")
	}
	return c.run(ctx, false, func() error {
		info, err := c.storageInfo(ctx, SerialFlash)
		if err != nil {
			return err
		}
		if size > info.Size() {
			return errors.New("format size exceeds device storage")
		}
		if _, err = c.command(ctx, words(0x28, 2, uint32(size/BlockSize), 0, 0, 2), 0, eraseTimeout); err != nil {
			return err
		}
		return c.status(ctx)
	})
}
