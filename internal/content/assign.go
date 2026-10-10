// SPDX-License-Identifier: GPL-2.0-only
package content

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"

	"goteddycloud/internal/config"
	"goteddycloud/internal/taf"
)

// Assign copies a validated source before changing the tag mapping. It never
// modifies the selected library file, even when the tag had a stale mapping.
func (s *Store) Assign(ctx context.Context, snap, global config.Snapshot, ruid, source string, live bool) error {
	release, err := s.mutations.RLock(ctx)
	if err != nil {
		return err
	}
	defer release()
	name, err := UIDPath(ruid)
	if err != nil {
		return err
	}
	path, sourceName, err := ResolveSource(snap, global, source)
	if err != nil {
		return err
	}
	sourceRoot, err := s.root(path)
	if err != nil {
		return err
	}
	file, err := sourceRoot.Open(sourceName)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxCachedTAF {
		return errors.New("assignment source size/type")
	}
	if _, err := taf.Validate(&playlistReader{ctx: ctx, reader: file}); err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	root, err := s.root(snap.String("internal.contentdirfull"))
	if err != nil {
		return err
	}
	unlock, err := root.LockContext(ctx, name)
	if err != nil {
		return err
	}
	defer unlock()
	m, err := readMetadata(root, name+".json")
	if err != nil {
		return err
	}
	if err := root.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return err
	}
	if err := root.WriteReaderAtomic(ctx, name, file, 0600, MaxCachedTAF); err != nil {
		return err
	}
	m.Set("source", "")
	if live {
		m.Set("live", true)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return root.WriteAtomic(name+".json", append(data, '\n'), 0600)
}
