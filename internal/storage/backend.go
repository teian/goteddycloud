// SPDX-License-Identifier: GPL-2.0-only
package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
)

// File is the seekable file contract used by deployment storage. Implementations
// may stage writes locally; Sync and Close must report publication failures.
type File interface {
	io.Reader
	io.ReaderAt
	io.Writer
	io.WriterAt
	io.Seeker
	io.Closer
	Stat() (os.FileInfo, error)
	ReadDir(int) ([]os.DirEntry, error)
	Sync() error
	Chmod(os.FileMode) error
	Name() string
}

type directory interface {
	Open(string) (File, error)
	OpenFile(string, int, os.FileMode) (File, error)
	Stat(string) (os.FileInfo, error)
	Lstat(string) (os.FileInfo, error)
	Mkdir(string, os.FileMode) error
	MkdirAll(string, os.FileMode) error
	Remove(string) error
	Rename(string, string) error
	Name() string
	Close() error
}

type filesystemRoot struct{ *os.Root }

func (r filesystemRoot) Open(name string) (File, error) { return r.Root.Open(name) }
func (r filesystemRoot) OpenFile(name string, flag int, mode os.FileMode) (File, error) {
	return r.Root.OpenFile(name, flag, mode)
}

// Backend selects storage for a deployment. Paths retain their configured
// filesystem spelling; remote backends map them into a confined namespace.
type Backend interface {
	OpenRoot(string) (*Root, error)
	MkdirAll(string, os.FileMode) error
	Open(string) (File, error)
	Stat(string) (os.FileInfo, error)
	ReadFile(string) ([]byte, error)
}

type Filesystem struct{}

func (Filesystem) OpenRoot(path string) (*Root, error)          { return Open(path) }
func (Filesystem) MkdirAll(path string, mode os.FileMode) error { return os.MkdirAll(path, mode) }
func (Filesystem) Open(path string) (File, error)               { return os.Open(path) }
func (Filesystem) Stat(path string) (os.FileInfo, error)        { return os.Stat(path) }
func (Filesystem) ReadFile(path string) ([]byte, error)         { return os.ReadFile(path) }

// WithContext returns a borrowed view. Closing it does not close the owning root.
// Files opened through it retain this context for their subsequent remote reads.
func (r *Root) WithContext(ctx context.Context) *Root {
	if remote, ok := r.directory.(*s3Root); ok {
		clone := *remote
		clone.ctx = ctx
		return &Root{directory: &clone, identity: r.identity, caseInsensitive: r.caseInsensitive}
	}
	return &Root{directory: borrowedDirectory{r.directory}, identity: r.identity, caseInsensitive: r.caseInsensitive}
}

type borrowedDirectory struct{ directory }

func (borrowedDirectory) Close() error { return nil }

func readAll(b Backend, path string) ([]byte, error) {
	f, err := b.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}
func openParent(b Backend, path string) (*Root, string, error) {
	r, err := b.OpenRoot(filepath.Dir(path))
	return r, filepath.Base(path), err
}
