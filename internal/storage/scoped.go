// SPDX-License-Identifier: GPL-2.0-only
package storage

import (
	"os"
	"path/filepath"
	"sync/atomic"
)

// Scoped confines a workspace to one backend root. Explicit shared files may be
// read (e.g. device credentials), but their parent roots cannot be opened.
// Filesystem operations stay anchored in os.Root, including nested symlinks.
type Scoped struct {
	backend Backend
	base    string
	anchor  *Root
	shared  atomic.Pointer[map[string]bool]
}

func NewScoped(backend Backend, base string) (*Scoped, error) {
	absolute, err := filepath.Abs(base)
	if err != nil {
		return nil, err
	}
	if err := backend.MkdirAll(absolute, 0700); err != nil {
		return nil, err
	}
	root, err := backend.OpenRoot(absolute)
	if err != nil {
		return nil, err
	}
	s := &Scoped{backend: backend, base: absolute, anchor: root}
	s.AllowReadFiles(nil)
	return s, nil
}
func (s *Scoped) Close() error { return s.anchor.Close() }
func (s *Scoped) AllowReadFiles(files []string) {
	allowed := make(map[string]bool, len(files))
	for _, file := range files {
		if filepath.IsAbs(file) {
			allowed[filepath.Clean(file)] = true
		}
	}
	s.shared.Store(&allowed)
}
func (s *Scoped) relative(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", os.ErrPermission
	}
	relative, err := filepath.Rel(s.base, filepath.Clean(path))
	if err != nil || !filepath.IsLocal(relative) {
		return "", os.ErrPermission
	}
	return relative, nil
}
func (s *Scoped) OpenRoot(path string) (*Root, error) {
	relative, err := s.relative(path)
	if err != nil {
		return nil, err
	}
	if local, ok := s.anchor.directory.(filesystemRoot); ok {
		child, err := local.Root.OpenRoot(relative)
		if err != nil {
			return nil, err
		}
		identity, err := filepath.EvalSymlinks(filepath.Join(s.base, relative))
		if err != nil {
			child.Close()
			return nil, err
		}
		return &Root{directory: filesystemRoot{child}, identity: identity, caseInsensitive: s.anchor.caseInsensitive}, nil
	}
	return s.backend.OpenRoot(path)
}
func (s *Scoped) Open(path string) (File, error) {
	relative, err := s.relative(path)
	if err == nil {
		return s.anchor.Open(relative)
	}
	if (*s.shared.Load())[filepath.Clean(path)] {
		return s.backend.Open(path)
	}
	return nil, err
}
func (s *Scoped) Stat(path string) (os.FileInfo, error) {
	relative, err := s.relative(path)
	if err != nil {
		return nil, err
	}
	return s.anchor.Stat(relative)
}
func (s *Scoped) MkdirAll(path string, mode os.FileMode) error {
	relative, err := s.relative(path)
	if err != nil {
		return err
	}
	return s.anchor.MkdirAll(relative, mode)
}
func (s *Scoped) ReadFile(path string) ([]byte, error) { return readAll(s, path) }

var _ Backend = (*Scoped)(nil)
