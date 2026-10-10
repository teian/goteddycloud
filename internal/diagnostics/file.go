// SPDX-License-Identifier: GPL-2.0-only
package diagnostics

import (
	"bytes"
	"errors"
	"goteddycloud/internal/storage"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// File stops recording at its cap. Diagnostic failures must not break traffic.
// The deployment root confines paths and symlinks, including absolute settings.
type File struct {
	mu         sync.Mutex
	file       storage.File
	limit      int64
	stopped    bool
	syncWrites bool
}

func Open(base, path string, limit int64) (*File, error) {
	return OpenWithStorage(base, path, limit, storage.Filesystem{})
}
func OpenWithStorage(base, path string, limit int64, backend storage.Backend) (*File, error) {
	if limit <= 0 {
		return nil, errors.New("invalid diagnostic limit")
	}
	absolute, err := filepath.Abs(base)
	if err != nil {
		return nil, err
	}
	if filepath.IsAbs(path) {
		path, err = filepath.Rel(absolute, path)
		if err != nil {
			return nil, err
		}
	}
	if !filepath.IsLocal(path) {
		return nil, errors.New("diagnostic destination must be inside deployment")
	}
	root, err := backend.OpenRoot(absolute)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("diagnostic destination must be a regular file")
	}
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return nil, err
	}
	_, remote := backend.(*storage.S3)
	return &File{file: f, limit: limit, syncWrites: remote}, nil
}
func (f *File) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopped {
		return len(p), nil
	}
	info, err := f.file.Stat()
	if err != nil || info.Size()+int64(len(p)) > f.limit {
		f.stopped = true
		return len(p), nil
	}
	if _, err = io.Copy(f.file, bytes.NewReader(p)); err == nil && f.syncWrites {
		err = f.file.Sync()
	}
	if err != nil {
		f.stopped = true
	}
	return len(p), nil
}
func (f *File) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = true
	return f.file.Close()
}
