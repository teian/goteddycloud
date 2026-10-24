// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"goteddycloud/internal/config"
	"goteddycloud/internal/content"
)

// A newly downloaded edition can be absent from the catalog even though a tag
// explicitly identifies its library source. Use that link only as a fallback;
// conflicting models must never make directory ordering decide the title.
func (s *Server) linkedLibraryModels(ctx context.Context, snap config.Snapshot) (map[string]string, error) {
	root, _, err := s.Content.ManagedRoot(snap, "")
	if err != nil {
		return nil, err
	}
	root = root.WithContext(ctx)
	budget := indexLimit
	readDirectory := func(name string) ([]fs.DirEntry, error) {
		file, err := root.Open(name)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		entries, err := file.ReadDir(budget + 1)
		if err != nil && err != io.EOF {
			return nil, err
		}
		budget -= len(entries)
		if budget < 0 {
			return nil, errors.New("linked library model scan exceeds limit")
		}
		return entries, nil
	}
	directories, err := readDirectory(".")
	if err != nil {
		return nil, err
	}
	global := s.Settings.Snapshot("")
	library := filepath.Clean(snap.String("internal.librarydirfull"))
	models := make(map[string]string)
	for _, directory := range directories {
		if !directory.IsDir() || len(directory.Name()) != 8 || !hexPrefix(directory.Name(), 8) {
			continue
		}
		files, err := readDirectory(directory.Name())
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if file.IsDir() || len(file.Name()) != 13 || !strings.HasSuffix(file.Name(), ".json") || !hexPrefix(file.Name(), 8) {
				continue
			}
			ruid := directory.Name() + file.Name()[:8]
			metadata, err := s.Content.MetadataContext(ctx, snap, ruid)
			if err != nil || metadata.String("tonie_model") == "" || metadata.String("source") == "" {
				continue
			}
			path, name, err := content.ResolveSource(snap, global, metadata.String("source"))
			if err != nil || filepath.Clean(path) != library {
				continue
			}
			name = filepath.Clean(name)
			model := metadata.String("tonie_model")
			if previous, exists := models[name]; exists && !strings.EqualFold(previous, model) {
				models[name] = ""
			} else if !exists {
				models[name] = model
			}
		}
	}
	return models, nil
}
