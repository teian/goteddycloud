// SPDX-License-Identifier: GPL-2.0-only
package content

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"goteddycloud/internal/config"
	"goteddycloud/internal/storage"
	"goteddycloud/internal/taf"
)

const MaxCachedTAF = int64(1 << 30)

func (s *Store) DownloadLock(ctx context.Context, snap config.Snapshot, ruid string) (func(), error) {
	name, err := UIDPath(ruid)
	if err != nil {
		return nil, err
	}
	root, err := s.root(snap.String("internal.contentdirfull"))
	if err != nil {
		return nil, err
	}
	return root.LockContext(ctx, "download:"+name)
}

type CacheFile struct {
	file            storage.File
	root            *storage.Root
	name, temporary string
	size            int64
}

func (s *Store) BeginCache(snap config.Snapshot, ruid string) (*CacheFile, error) {
	name, err := UIDPath(ruid)
	if err != nil {
		return nil, err
	}
	root, err := s.root(snap.String("internal.contentdirfull"))
	if err != nil {
		return nil, err
	}
	return newCacheFile(root, name)
}
func newCacheFile(root *storage.Root, name string) (*CacheFile, error) {
	if err := root.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return nil, err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	temporary := filepath.Join(filepath.Dir(name), ".cache-"+hex.EncodeToString(nonce[:]))
	file, err := root.OpenFile(temporary, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	return &CacheFile{file: file, root: root, name: name, temporary: temporary}, nil
}
func (c *CacheFile) Write(data []byte) (int, error) {
	if int64(len(data)) > MaxCachedTAF-c.size {
		return 0, errors.New("content cache size limit")
	}
	n, err := c.file.Write(data)
	c.size += int64(n)
	return n, err
}
func (c *CacheFile) Close() error {
	err := c.file.Close()
	if errors.Is(err, os.ErrClosed) {
		err = nil
	}
	remove := c.root.Remove(c.temporary)
	if errors.Is(remove, os.ErrNotExist) {
		remove = nil
	}
	return errors.Join(err, remove)
}
func (c *CacheFile) Commit(ctx context.Context, expected int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if expected >= 0 && expected != c.size {
		return io.ErrUnexpectedEOF
	}
	if _, err := c.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := taf.Validate(c.file); err != nil {
		return err
	}
	if _, err := c.file.Seek(taf.BlockSize, io.SeekStart); err != nil {
		return err
	}
	if err := taf.CheckPages(c.file); err != nil {
		return err
	}
	if err := c.file.Sync(); err != nil {
		return err
	}
	unlock, err := c.root.LockContext(ctx, c.name)
	if err != nil {
		return err
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	// Close before rename for Windows, where open files cannot be replaced.
	if err := c.file.Close(); err != nil {
		return err
	}
	return c.root.WithContext(ctx).Rename(c.temporary, c.name)
}

// CacheToLibrary maps a validated UID cache to the reference audio-ID path.
// A conflicting valid library file is retained, and the UID cache stays usable.
func (s *Store) CacheToLibrary(ctx context.Context, snap config.Snapshot, ruid string) error {
	return s.MigrateToLibrary(ctx, snap, ruid, false)
}

func (s *Store) MigrateToLibrary(ctx context.Context, snap config.Snapshot, ruid string, atRoot bool) error {
	name, err := UIDPath(ruid)
	if err != nil {
		return err
	}
	root, err := s.root(snap.String("internal.contentdirfull"))
	if err != nil {
		return err
	}
	source, err := root.Open(name)
	if err != nil {
		return err
	}
	defer source.Close()
	header, err := taf.Validate(source)
	if err != nil {
		return err
	}
	if header.GetAudioId() <= 1 && !atRoot {
		return errors.New("reserved audio ID cannot move to cache library")
	}
	library, err := s.root(snap.String("internal.librarydirfull"))
	if err != nil {
		return err
	}
	target := fmt.Sprintf("by/audioID/%d.taf", header.GetAudioId())
	if atRoot {
		target = fmt.Sprintf("%d.taf", header.GetAudioId())
	}
	// Separate coordination key prevents two UIDs with the same ID from replacing
	// each other's different payloads while the actual publication lock is held.
	unlock, err := library.LockContext(ctx, "migration:"+target)
	if err != nil {
		return err
	}
	defer unlock()
	exists := false
	if file, err := library.Open(target); err == nil {
		old, validation := taf.Validate(file)
		file.Close()
		if validation == nil {
			if !bytes.Equal(old.Sha1Hash, header.Sha1Hash) {
				return errors.New("library audio-ID collision")
			}
			exists = true
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if !exists {
		output, err := newCacheFile(library, target)
		if err != nil {
			return err
		}
		defer output.Close()
		if _, err := source.Seek(0, io.SeekStart); err != nil {
			return err
		}
		if _, err := io.Copy(output, source); err != nil {
			return err
		}
		if err := output.Commit(ctx, int64(header.GetNumBytes())+taf.BlockSize); err != nil {
			return err
		}
	}
	if err := s.UpdateContext(ctx, snap, ruid, func(m Metadata) error { m.Set("source", "lib://"+target); return nil }); err != nil {
		return err
	}
	source.Close()
	return root.Remove(name)
}
