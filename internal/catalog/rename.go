// SPDX-License-Identifier: GPL-2.0-only
package catalog

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

const RenameJournal = "tonies.custom.rename.json"

type renameIntent struct {
	From        string `json:"from"`
	To          string `json:"to"`
	ContentRoot string `json:"content_root"`
	Linked      bool   `json:"linked"`
}

func (c *Catalog) pendingRename() bool {
	file, err := c.root.Open(RenameJournal)
	if err == nil {
		file.Close()
		return true
	}
	return !errors.Is(err, os.ErrNotExist)
}
func renameEntries(entries []Entry, from, to string) ([]Entry, error) {
	fromIndex := -1
	for index, entry := range entries {
		if strings.EqualFold(entry.String("model"), from) {
			fromIndex = index
		}
		if strings.EqualFold(entry.String("model"), to) {
			return nil, mutationError(InvalidMutation, "Target model already exists")
		}
	}
	if fromIndex < 0 {
		return nil, mutationError(MissingModel, "Source model not found")
	}
	put(entries[fromIndex], "model", to)
	return normalizeCustom(entries)
}
func (c *Catalog) Rename(from, to, contentRoot string, linked bool, keep uint64, now time.Time, updateLinks func(string, string) error) error {
	if strings.EqualFold(from, to) {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pendingRename() {
		return mutationError(RenamePending, "Pending custom model rename must be recovered")
	}
	entries, err := c.read("tonies.custom.json")
	if err != nil {
		return mutationError(CatalogReadFailure, "Failed to load tonies.custom.json")
	}
	renamed, err := renameEntries(entries, from, to)
	if err != nil {
		return err
	}
	intent := renameIntent{From: from, To: to, ContentRoot: contentRoot, Linked: linked}
	data, _ := json.Marshal(intent)
	if err := c.root.WriteAtomic(RenameJournal, data, 0600); err != nil {
		return mutationError(CatalogWriteFailure, "Failed to journal model rename")
	}
	if linked {
		if err := updateLinks(from, to); err != nil {
			return mutationError(RenameRecoveryFailure, "Failed to update linked content; rename recovery pending")
		}
	}
	if err := c.saveCustom(renamed, keep, now); err != nil {
		return err
	}
	return c.root.Remove(RenameJournal)
}

// RecoverRename rolls a journaled intent forward before accepting requests.
// A changed configured root is an explicit startup error, never an implicit write
// through an arbitrary path embedded in the journal.
func (c *Catalog) RecoverRename(contentRoot string, keep uint64, now time.Time, updateLinks func(string, string) error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	file, err := c.root.Open(RenameJournal)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(file, (2<<20)+1))
	file.Close()
	if err != nil || len(data) > 2<<20 {
		return errors.New("invalid custom model rename journal")
	}
	var intent renameIntent
	if err := json.Unmarshal(data, &intent); err != nil || intent.From == "" || intent.To == "" || strings.EqualFold(intent.From, intent.To) {
		return errors.New("invalid custom model rename journal")
	}
	if intent.ContentRoot != contentRoot {
		return errors.New("pending model rename refers to a different configured content root")
	}
	entries, err := c.read("tonies.custom.json")
	if err != nil {
		return err
	}
	source, target := false, false
	for _, entry := range entries {
		source = source || strings.EqualFold(entry.String("model"), intent.From)
		target = target || strings.EqualFold(entry.String("model"), intent.To)
	}
	if source == target {
		return errors.New("pending model rename cannot reconcile catalog models")
	}
	if intent.Linked {
		if err := updateLinks(intent.From, intent.To); err != nil {
			return err
		}
	}
	if source {
		renamed, err := renameEntries(entries, intent.From, intent.To)
		if err != nil {
			return err
		}
		if err := c.saveCustom(renamed, keep, now); err != nil {
			return err
		}
	}
	return c.root.Remove(RenameJournal)
}
