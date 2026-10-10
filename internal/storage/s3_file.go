// SPDX-License-Identifier: GPL-2.0-only
package storage

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sync"
)

// s3File uses conditional ranged reads against the opened object version. A
// bounded read-ahead buffer avoids a network request for each TAF block.
type s3File struct {
	mu                   sync.Mutex
	root                 *s3Root
	key, name            string
	info                 objectInfo
	etag, version        string
	local                *os.File
	writable             bool
	readable             bool
	exclusive            bool
	appendMode           bool
	dirty                bool
	closed               bool
	dir                  bool
	position, cacheStart int64
	cache                []byte
	entries              []os.DirEntry
	listed               bool
	entryPosition        int
}

func (f *s3File) Name() string { return f.name }
func (f *s3File) Stat() (os.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, os.ErrClosed
	}
	if f.local != nil {
		info, e := f.local.Stat()
		if e != nil {
			return nil, e
		}
		result := f.info
		result.size = info.Size()
		return result, nil
	}
	return f.info, nil
}
func (f *s3File) readAt(p []byte, off int64) (int, error) {
	if f.closed {
		return 0, os.ErrClosed
	}
	if off < 0 {
		return 0, errors.New("negative offset")
	}
	if len(p) == 0 {
		return 0, nil
	}
	if f.dir {
		return 0, errors.New("is a directory")
	}
	if !f.readable {
		return 0, fs.ErrPermission
	}
	if f.local != nil {
		return f.local.ReadAt(p, off)
	}
	if off >= f.info.size {
		return 0, io.EOF
	}
	total := 0
	for len(p) > 0 && off < f.info.size {
		if off < f.cacheStart || off >= f.cacheStart+int64(len(f.cache)) || len(f.cache) == 0 {
			end := min(off+(1<<20)-1, f.info.size-1)
			out, err := f.root.backend.client.Get(f.root.ctx, f.key, f.etag, f.version, fmt.Sprintf("bytes=%d-%d", off, end))
			if err != nil {
				return total, s3Error("read", f.name, err)
			}
			data, e := io.ReadAll(io.LimitReader(out, end-off+2))
			e = errors.Join(e, out.Close())
			if e != nil {
				return total, e
			}
			if int64(len(data)) != end-off+1 {
				return total, io.ErrUnexpectedEOF
			}
			f.cacheStart = off
			f.cache = data
		}
		n := copy(p, f.cache[off-f.cacheStart:])
		total += n
		off += int64(n)
		p = p[n:]
	}
	if len(p) > 0 {
		return total, io.EOF
	}
	return total, nil
}
func (f *s3File) ReadAt(p []byte, off int64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readAt(p, off)
}
func (f *s3File) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, err := f.readAt(p, f.position)
	f.position += int64(n)
	return n, err
}
func (f *s3File) Seek(off int64, whence int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, os.ErrClosed
	}
	size := f.info.size
	if f.local != nil {
		info, e := f.local.Stat()
		if e != nil {
			return 0, e
		}
		size = info.Size()
	}
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		off += f.position
	case io.SeekEnd:
		off += size
	default:
		return 0, errors.New("invalid whence")
	}
	if off < 0 {
		return 0, errors.New("negative offset")
	}
	f.position = off
	return off, nil
}
func (f *s3File) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, os.ErrClosed
	}
	if !f.writable {
		return 0, fs.ErrPermission
	}
	if f.appendMode {
		info, e := f.local.Stat()
		if e != nil {
			return 0, e
		}
		f.position = info.Size()
	}
	n, e := f.local.WriteAt(p, f.position)
	f.position += int64(n)
	f.dirty = f.dirty || n > 0
	return n, e
}
func (f *s3File) WriteAt(p []byte, off int64) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, os.ErrClosed
	}
	if !f.writable {
		return 0, fs.ErrPermission
	}
	if f.appendMode {
		return 0, errors.New("WriteAt with O_APPEND")
	}
	n, e := f.local.WriteAt(p, off)
	f.dirty = f.dirty || n > 0
	return n, e
}
func (f *s3File) Chmod(mode os.FileMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return os.ErrClosed
	}
	if !f.writable {
		return fs.ErrPermission
	}
	f.info.mode = mode.Perm()
	f.dirty = true
	return nil
}
func (f *s3File) sync() error {
	if !f.dirty {
		return nil
	}
	info, err := f.local.Stat()
	if err != nil {
		return err
	}
	if info.Size() > 5_000_000_000 {
		return errors.New("S3 staged file exceeds single-object upload limit")
	}
	_, err = f.local.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}
	err = f.root.backend.client.Put(f.root.ctx, f.key, f.local, info.Size(), map[string]string{"mode": fmt.Sprintf("%o", f.info.mode.Perm())}, f.exclusive)
	if err == nil {
		f.dirty = false
		f.exclusive = false
	}
	return s3Error("sync", f.name, err)
}
func (f *s3File) Sync() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return os.ErrClosed
	}
	return f.sync()
}
func (f *s3File) discard() error {
	if f.local == nil {
		return nil
	}
	return errors.Join(f.local.Close(), os.Remove(f.local.Name()))
}
func (f *s3File) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return os.ErrClosed
	}
	f.closed = true
	err := f.sync()
	f.cache = nil
	return errors.Join(err, f.discard())
}
func (f *s3File) ReadDir(n int) ([]os.DirEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, os.ErrClosed
	}
	if !f.dir {
		return nil, errors.New("not a directory")
	}
	if !f.listed {
		entries, err := f.root.list(f.name)
		if err != nil {
			return nil, err
		}
		f.entries = entries
		f.listed = true
	}
	if n > 0 && f.entryPosition >= len(f.entries) {
		return nil, io.EOF
	}
	end := len(f.entries)
	if n > 0 {
		end = min(end, f.entryPosition+n)
	}
	result := append([]os.DirEntry{}, f.entries[f.entryPosition:end]...)
	f.entryPosition = end
	return result, nil
}
