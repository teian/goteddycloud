// SPDX-License-Identifier: GPL-2.0-only
package content

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"goteddycloud/internal/audio"
	"goteddycloud/internal/config"
	"goteddycloud/internal/taf"
)

type Playlist struct {
	Type     string         `json:"type"`
	AudioID  uint32         `json:"audio_id"`
	Filepath string         `json:"filepath"`
	Name     string         `json:"name"`
	Files    []PlaylistFile `json:"files"`
}
type PlaylistFile struct {
	Filepath string `json:"filepath"`
	Name     string `json:"name"`
}

var playlistAdmission = make(chan struct{}, 4)

func (s *Store) readPlaylist(snap, global config.Snapshot, source string) (*Playlist, error) {
	path, name, err := ResolveSource(snap, global, source)
	if err != nil {
		return nil, err
	}
	root, err := s.root(path)
	if err != nil {
		return nil, err
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 {
		return nil, errors.New("playlist exceeds limit")
	}
	var playlist Playlist
	if err := json.Unmarshal(data, &playlist); err != nil {
		return nil, err
	}
	if playlist.Type != "tap" || playlist.Filepath == "" || len(playlist.Files) == 0 || len(playlist.Files) > 99 {
		return nil, errors.New("invalid TAP playlist")
	}
	if _, _, err := ResolveSource(snap, global, playlist.Filepath); err != nil {
		return nil, err
	}
	for _, file := range playlist.Files {
		if file.Filepath == "" {
			return nil, errors.New("empty playlist source")
		}
		if _, _, err := ResolveSource(snap, global, file.Filepath); err != nil && !streamURL(file.Filepath) {
			return nil, err
		}
	}
	return &playlist, nil
}

// EnsurePlaylist converts a local TAP source when its cached TAF is absent,
// invalid, or has the wrong identity. Ordinary stored TAF sources are untouched.
// Opened confined inputs are copied into a private directory before FFmpeg, so
// FFmpeg never follows an untrusted playlist path outside configured roots.
func (s *Store) EnsurePlaylist(ctx context.Context, snap, global config.Snapshot, ruid string) error {
	metadata, err := s.Metadata(snap, ruid)
	if err != nil {
		return err
	}
	source := metadata.String("source")
	if source == "" {
		return nil
	}
	// Inspect only JSON-looking sources. File extensions need not be .tap.
	path, name, err := ResolveSource(snap, global, source)
	if err != nil {
		return err
	}
	root, err := s.root(path)
	if err != nil {
		return err
	}
	file, err := root.Open(name)
	if err != nil {
		return err
	}
	reader := bufio.NewReader(file)
	isObject := false
	for count := 0; count < 1<<20; count++ {
		value, readErr := reader.ReadByte()
		if readErr != nil {
			break
		}
		if value == ' ' || value == '\n' || value == '\r' || value == '\t' {
			continue
		}
		isObject = value == '{'
		break
	}
	file.Close()
	if !isObject {
		return nil
	}
	playlist, err := s.readPlaylist(snap, global, source)
	if err != nil {
		return err
	}
	select {
	case playlistAdmission <- struct{}{}:
		defer func() { <-playlistAdmission }()
	default:
		return errors.New("playlist conversion queue is full")
	}
	outputPath, outputName, err := ResolveSource(snap, global, playlist.Filepath)
	if err != nil {
		return err
	}
	outputRoot, err := s.root(outputPath)
	if err != nil {
		return err
	}
	unlock, err := outputRoot.LockContext(ctx, outputName)
	if err != nil {
		return err
	}
	defer unlock()
	if existing, err := outputRoot.Open(outputName); err == nil {
		header, validationErr := taf.Validate(existing)
		existing.Close()
		if validationErr == nil && header.GetAudioId() == playlist.AudioID {
			return nil
		}
	}
	temporary, err := os.MkdirTemp("", "goteddycloud-playlist-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	const maxBytes = int64(1 << 30)
	remaining := maxBytes
	sources := make([]string, 0, len(playlist.Files))
	for index, item := range playlist.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		rootPath, relative, err := ResolveSource(snap, global, item.Filepath)
		if err != nil {
			return err
		}
		inputRoot, err := s.root(rootPath)
		if err != nil {
			return err
		}
		input, err := inputRoot.Open(relative)
		if err != nil {
			return err
		}
		info, err := input.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > remaining {
			input.Close()
			return errors.New("playlist input exceeds bounds or is not a regular file")
		}
		destination := filepath.Join(temporary, playlistInputName(index, filepath.Ext(relative)))
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			input.Close()
			return err
		}
		count, copyErr := io.Copy(output, io.LimitReader(playlistReader{ctx, input}, remaining+1))
		closeErr := output.Close()
		input.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if count > remaining {
			return errors.New("playlist inputs exceed size limit")
		}
		remaining -= count
		sources = append(sources, destination)
	}
	result := filepath.Join(temporary, "result.taf")
	if err := audio.Convert(ctx, result, playlist.AudioID, sources); err != nil {
		return err
	}
	converted, err := os.Open(result)
	if err != nil {
		return err
	}
	defer converted.Close()
	if _, err := taf.Validate(converted); err != nil {
		return err
	}
	if _, err := converted.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := outputRoot.MkdirAll(filepath.Dir(outputName), 0700); err != nil {
		return err
	}
	return outputRoot.WriteReaderAtomic(ctx, outputName, converted, 0600, maxBytes)
}
func playlistInputName(index int, extension string) string {
	// Extensions are used only for codec probing in a private directory.
	if len(extension) > 20 || strings.ContainsAny(extension, "/\\") {
		extension = ""
	}
	return fmt.Sprintf("%03d%s", index, extension)
}

type playlistReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r playlistReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}
