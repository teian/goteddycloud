// SPDX-License-Identifier: GPL-2.0-only
package content

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"

	"goteddycloud/internal/config"
)

// RenameModels is idempotent so a persisted rename intent can be replayed after
// a crash. Other metadata mutations wait until the linked update completes.
func (s *Store) RenameModels(ctx context.Context, snap config.Snapshot, from, to string) error {
	release, err := s.mutations.Lock(ctx)
	if err != nil {
		return err
	}
	defer release()
	root, err := s.root(snap.String("internal.contentdirfull"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	visited := 0
	visit := func(directory string, callback func(fs.DirEntry) error) error {
		file, err := root.Open(directory)
		if err != nil {
			return err
		}
		defer file.Close()
		for {
			entries, readErr := file.ReadDir(256)
			for _, entry := range entries {
				if err := ctx.Err(); err != nil {
					return err
				}
				visited++
				if visited > 100000 {
					return errors.New("linked model scan exceeds limit")
				}
				if err := callback(entry); err != nil {
					return err
				}
			}
			if readErr == io.EOF {
				return nil
			}
			if readErr != nil {
				return readErr
			}
		}
	}
	return visit(".", func(directory fs.DirEntry) error {
		if !directory.IsDir() || len(directory.Name()) != 8 {
			return nil
		}
		if _, err := UIDPath(directory.Name() + "00000000"); err != nil {
			return nil
		}
		return visit(directory.Name(), func(file fs.DirEntry) error {
			if file.IsDir() || len(file.Name()) != 13 || !strings.HasSuffix(file.Name(), ".json") {
				return nil
			}
			ruid := directory.Name() + strings.TrimSuffix(file.Name(), ".json")
			if _, err := UIDPath(ruid); err != nil {
				return nil
			}
			name := directory.Name() + "/" + strings.TrimSuffix(file.Name(), ".json")
			metadata, err := readMetadata(root, name+".json")
			if err != nil {
				return err
			}
			if !strings.EqualFold(metadata.String("tonie_model"), from) {
				return nil
			}
			return updateMetadata(ctx, root, name, func(m Metadata) error {
				if strings.EqualFold(m.String("tonie_model"), from) {
					m.Set("tonie_model", to)
				}
				return nil
			})
		})
	})
}
