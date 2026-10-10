// SPDX-License-Identifier: GPL-2.0-only
package content

import (
	"context"
	"goteddycloud/internal/storage"
	"io"
	"os"

	"goteddycloud/internal/config"
	"goteddycloud/internal/protocol"
	"goteddycloud/internal/taf"
)

// Inspect selects the same local source as delivery without starting conversion
// or cloud downloads. A nil header with a nonnil file means existing invalid
// audio; a missing file remains distinct from invalid audio in tag listings.
func (s *Store) Inspect(snap, global config.Snapshot, ruid string) (storage.File, *protocol.TonieboxAudioFileHeader, error) {
	return s.InspectContext(context.Background(), snap, global, ruid)
}
func (s *Store) InspectContext(ctx context.Context, snap, global config.Snapshot, ruid string) (storage.File, *protocol.TonieboxAudioFileHeader, error) {
	name, err := UIDPath(ruid)
	if err != nil {
		return nil, nil, err
	}
	root, err := s.root(snap.String("internal.contentdirfull"))
	if err != nil {
		return nil, nil, err
	}
	unlock, err := root.LockContext(ctx, name)
	if err != nil {
		return nil, nil, err
	}
	file, err := func() (storage.File, error) {
		defer unlock()
		metadata, err := readMetadata(root.WithContext(ctx), name+".json")
		if err != nil {
			return nil, err
		}
		source := metadata.String("source")
		if source != "" {
			rootPath, sourceName, err := ResolveSource(snap, global, source)
			if err != nil {
				return nil, err
			}
			if playlist, playlistErr := s.readPlaylist(snap, global, source); playlistErr == nil {
				rootPath, sourceName, err = ResolveSource(snap, global, playlist.Filepath)
				if err != nil {
					return nil, err
				}
			}
			root, err = s.root(rootPath)
			if err != nil {
				return nil, err
			}
			name = sourceName
		}
		file, err := root.WithContext(ctx).Open(name)
		if err != nil {
			return nil, err
		}
		return file, nil
	}()
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		if err == nil {
			err = os.ErrNotExist
		}
		return nil, nil, err
	}
	header, err := taf.ReadHeader(file)
	if err == nil && snap.Bool("core.full_taf_validation") {
		file.Seek(0, io.SeekStart)
		header, err = taf.Validate(&playlistReader{ctx: ctx, reader: file})
	}
	if err != nil {
		header = nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, nil, err
	}
	return file, header, nil
}
