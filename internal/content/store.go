// SPDX-License-Identifier: GPL-2.0-only
package content

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"goteddycloud/internal/config"
	"goteddycloud/internal/protocol"
	"goteddycloud/internal/storage"
	"goteddycloud/internal/syncutil"
	"goteddycloud/internal/taf"
)

type Metadata map[string]json.RawMessage

func (m Metadata) Bool(key string) bool      { var v bool; _ = json.Unmarshal(m[key], &v); return v }
func (m Metadata) String(key string) string  { var v string; _ = json.Unmarshal(m[key], &v); return v }
func (m Metadata) Set(key string, value any) { b, _ := json.Marshal(value); m[key] = b }
func defaults() Metadata {
	m := Metadata{}
	for _, k := range []string{"live", "nocloud", "cache", "cloud_override", "hide", "claimed"} {
		m.Set(k, false)
	}
	for _, k := range []string{"source", "cloud_ruid", "cloud_auth", "tonie_model"} {
		m.Set(k, "")
	}
	m.Set("skip_seconds", 0)
	return m
}
func UIDPath(ruid string) (string, error) {
	if len(ruid) != 16 {
		return "", errors.New("UID must contain sixteen hexadecimal digits")
	}
	if _, err := hex.DecodeString(ruid); err != nil {
		return "", errors.New("invalid UID")
	}
	return strings.ToUpper(ruid[:8] + "/" + ruid[8:]), nil
}
func RUID(uid uint64) string {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uid)
	return hex.EncodeToString(b[:])
}

type Store struct {
	mutations syncutil.RWGate
	mu        sync.Mutex
	roots     map[string]*storage.Root
	closed    bool
	backend   storage.Backend
}

func New() *Store { return NewWithStorage(storage.Filesystem{}) }
func NewWithStorage(backend storage.Backend) *Store {
	return &Store{backend: backend, roots: map[string]*storage.Root{}}
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	var err error
	for _, r := range s.roots {
		err = errors.Join(err, r.Close())
	}
	return err
}
func (s *Store) root(path string) (*storage.Root, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	// Canonical resource identity is established by storage.Open on the cold path.
	// Cached reads need no filesystem lookup or global mutex held during open.
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, os.ErrClosed
	}
	existing := s.roots[path]
	s.mu.Unlock()
	if existing != nil {
		return existing, nil
	}
	root, err := s.backend.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		root.Close()
		return nil, os.ErrClosed
	}
	if existing := s.roots[path]; existing != nil {
		root.Close()
		return existing, nil
	}
	s.roots[path] = root
	return root, nil
}
func (s *Store) Metadata(snap config.Snapshot, ruid string) (Metadata, error) {
	return s.MetadataContext(context.Background(), snap, ruid)
}
func (s *Store) MetadataContext(ctx context.Context, snap config.Snapshot, ruid string) (Metadata, error) {
	name, err := UIDPath(ruid)
	if err != nil {
		return nil, err
	}
	root, err := s.root(snap.String("internal.contentdirfull"))
	if err != nil {
		return nil, err
	}
	return readMetadata(root.WithContext(ctx), name+".json")
}
func readMetadata(root *storage.Root, name string) (Metadata, error) {
	file, err := root.Open(name)
	if errors.Is(err, os.ErrNotExist) {
		return defaults(), nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 {
		return nil, errors.New("metadata exceeds limit")
	}
	m := defaults()
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("content metadata: %w", err)
	}
	if m == nil {
		return nil, errors.New("metadata must be an object")
	}
	return m, nil
}
func (s *Store) Update(snap config.Snapshot, ruid string, change func(Metadata) error) error {
	return s.UpdateContext(context.Background(), snap, ruid, change)
}
func (s *Store) UpdateContext(ctx context.Context, snap config.Snapshot, ruid string, change func(Metadata) error) error {
	release, err := s.mutations.RLock(ctx)
	if err != nil {
		return err
	}
	defer release()
	return s.update(ctx, snap, ruid, change)
}
func (s *Store) update(ctx context.Context, snap config.Snapshot, ruid string, change func(Metadata) error) error {
	name, err := UIDPath(ruid)
	if err != nil {
		return err
	}
	root, err := s.root(snap.String("internal.contentdirfull"))
	if err != nil {
		return err
	}
	return updateMetadata(ctx, root, name, change)
}
func updateMetadata(ctx context.Context, root *storage.Root, name string, change func(Metadata) error) error {
	unlock, err := root.LockContext(ctx, name)
	if err != nil {
		return err
	}
	defer unlock()
	m, err := readMetadata(root.WithContext(ctx), name+".json")
	if err != nil {
		return err
	}
	if err := change(m); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := root.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return err
	}
	return root.WriteReaderAtomic(ctx, name+".json", bytes.NewReader(append(data, '\n')), 0600, int64(len(data)+1))
}

// ResolveSource confines each symbolic path to its configured root. Absolute or
// ordinary paths are accepted only when contained in a configured content root.
func ResolveSource(snap, global config.Snapshot, source string) (string, string, error) {
	mappings := []struct{ prefix, path string }{{"data://", snap.String("internal.datadirfull")}, {"content://", snap.String("internal.contentdirfull")}, {"lib://", snap.String("internal.librarydirfull")}, {"data-global://", global.String("internal.datadirfull")}, {"content-global://", global.String("internal.contentdirfull")}, {"lib-global://", global.String("internal.librarydirfull")}}
	for _, m := range mappings {
		if tail, ok := strings.CutPrefix(source, m.prefix); ok {
			if !filepath.IsLocal(tail) {
				return "", "", errors.New("source escapes root")
			}
			return m.path, tail, nil
		}
	}
	absolute := source
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(snap.Base, source)
	}
	for _, m := range mappings {
		relative, err := filepath.Rel(m.path, absolute)
		if err == nil && filepath.IsLocal(relative) {
			return m.path, relative, nil
		}
	}
	return "", "", errors.New("source is outside configured roots")
}
func (s *Store) Open(snap, global config.Snapshot, ruid string) (storage.File, *protocol.TonieboxAudioFileHeader, error) {
	return s.OpenContext(context.Background(), snap, global, ruid)
}
func (s *Store) OpenContext(ctx context.Context, snap, global config.Snapshot, ruid string) (storage.File, *protocol.TonieboxAudioFileHeader, error) {
	name, err := UIDPath(ruid)
	if err != nil {
		return nil, nil, err
	}
	// Keep metadata selection and the file open together with pointer updates.
	// Otherwise migration can remove a UID file after this reader saw the old
	// source mapping, yielding a spurious 404 during a successful cache fill.
	contentRoot, err := s.root(snap.String("internal.contentdirfull"))
	if err != nil {
		return nil, nil, err
	}
	unlock, err := contentRoot.LockContext(ctx, name)
	if err != nil {
		return nil, nil, err
	}
	file, metadata, err := func() (storage.File, Metadata, error) {
		defer unlock()
		metadata, err := s.MetadataContext(ctx, snap, ruid)
		if err != nil {
			return nil, nil, err
		}
		rootPath := snap.String("internal.contentdirfull")
		if source := metadata.String("source"); source != "" {
			rootPath, name, err = ResolveSource(snap, global, source)
			if err != nil {
				return nil, nil, err
			}
		}
		root, err := s.root(rootPath)
		if err != nil {
			return nil, nil, err
		}
		file, err := root.WithContext(ctx).Open(name)
		if err != nil {
			return nil, nil, err
		}
		return file, metadata, nil
	}()
	if err != nil {
		return nil, nil, err
	}
	header, err := taf.Validate(&playlistReader{ctx: ctx, reader: file})
	if err != nil {
		file.Close()
		playlist, playlistErr := s.readPlaylist(snap, global, metadata.String("source"))
		if playlistErr != nil {
			return nil, nil, err
		}
		outputPath, outputName, resolveErr := ResolveSource(snap, global, playlist.Filepath)
		if resolveErr != nil {
			return nil, nil, resolveErr
		}
		outputRoot, rootErr := s.root(outputPath)
		if rootErr != nil {
			return nil, nil, rootErr
		}
		file, err = outputRoot.WithContext(ctx).Open(outputName)
		if err != nil {
			return nil, nil, err
		}
		header, err = taf.Validate(&playlistReader{ctx: ctx, reader: file})
		if err != nil {
			file.Close()
			return nil, nil, err
		}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, nil, err
	}
	return file, header, nil
}

func UIDFromRUID(ruid string) (uint64, error) {
	if _, err := UIDPath(ruid); err != nil {
		return 0, err
	}
	data, err := hex.DecodeString(ruid)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(data), nil
}

// SourceRoot shares the same confined root and mutation locks as management APIs.
func (s *Store) SourceRoot(snap, global config.Snapshot, source string) (*storage.Root, string, error) {
	path, name, err := ResolveSource(snap, global, source)
	if err != nil {
		return nil, "", err
	}
	root, err := s.root(path)
	return root, name, err
}
