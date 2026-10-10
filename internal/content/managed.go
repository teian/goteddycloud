// SPDX-License-Identifier: GPL-2.0-only
package content

import (
	"goteddycloud/internal/config"
	"goteddycloud/internal/storage"
)

// ManagedRoot borrows the store's shared confined root and resource locks.
// Callers must not close it; Store.Close owns its lifetime.
func (s *Store) ManagedRoot(snap config.Snapshot, special string) (*storage.Root, string, error) {
	path := snap.String("internal.contentdirfull")
	prefix := ""
	switch special {
	case "library":
		path = snap.String("internal.librarydirfull")
	case "custom_img":
		path = snap.String("internal.wwwdirfull")
		prefix = "custom_img"
	}
	root, err := s.root(path)
	if err != nil {
		return nil, "", err
	}
	if prefix != "" {
		if err := root.MkdirAll(prefix, 0700); err != nil {
			return nil, "", err
		}
	}
	return root, prefix, nil
}
