// SPDX-License-Identifier: GPL-2.0-only
package catalog

import (
	"fmt"
	"io"

	"goteddycloud/internal/storage"
)

// SeedBaseline fills missing or empty official databases from an immutable image
// snapshot. Populated deployment databases and custom entries always win.
func (c *Catalog) SeedBaseline(directory string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	names := []string{"tonies.json", "tonieboxes.json"}
	needed := []string{}
	for _, name := range names {
		entries, err := c.read(name)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			needed = append(needed, name)
		}
	}
	if len(needed) == 0 {
		return nil
	}
	root, err := storage.Open(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	data := make(map[string][]byte, len(needed))
	parsed := make(map[string][]Entry, len(needed))
	// Validate all needed inputs before publishing anything.
	for _, name := range needed {
		file, err := root.Open(name)
		if err != nil {
			return err
		}
		payload, err := io.ReadAll(io.LimitReader(file, (64<<20)+1))
		file.Close()
		if err != nil {
			return err
		}
		entries, err := parse(name, payload)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return fmt.Errorf("bundled %s: empty catalog", name)
		}
		data[name], parsed[name] = payload, entries
	}
	for _, name := range needed {
		if err := c.root.WriteAtomic(name, data[name], 0600); err != nil {
			return err
		}
		if name == "tonies.json" {
			current := c.active.Load()
			c.active.Store(newSnapshot(parsed[name], current.Custom, current.V2))
		}
	}
	return nil
}
