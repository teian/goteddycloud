// SPDX-License-Identifier: GPL-2.0-only
package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func customEntry(model string) Entry {
	entry := Entry{}
	put(entry, "model", model)
	put(entry, "series", "Synthetic series")
	put(entry, "release", 2026)
	entry["extension"] = json.RawMessage(`{"preserved":true}`)
	return entry
}
func TestCustomConcurrentUpdatesNormalizationRetentionAndRestart(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "tonies.custom.json"), []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	var group sync.WaitGroup
	for index := range 10 {
		group.Go(func() {
			if err := c.Upsert([]Entry{customEntry(fmt.Sprintf("MODEL_%02d", index))}, 2, now.Add(time.Duration(index)*time.Second)); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if len(c.Entries(true)) != 10 {
		t.Fatal("concurrent updates lost")
	}
	backups, _ := filepath.Glob(filepath.Join(directory, "tonies.custom.json.*.bak"))
	if len(backups) != 2 {
		t.Fatal(backups)
	}
	entry := c.ByModel("model_05")
	if entry.String("release") != "2026" || string(entry["hash"]) != "[]" || string(entry["extension"]) != `{"preserved":true}` {
		t.Fatal(entry)
	}
	before, _ := os.ReadFile(filepath.Join(directory, "tonies.custom.json"))
	invalid := customEntry("INVALID")
	put(invalid, "tracks", []any{42})
	if err := c.Upsert([]Entry{invalid}, 2, now.Add(time.Minute)); err == nil {
		t.Fatal("invalid tracks accepted")
	}
	after, _ := os.ReadFile(filepath.Join(directory, "tonies.custom.json"))
	if !bytes.Equal(before, after) || c.ByModel("INVALID") != nil {
		t.Fatal("invalid mutation published")
	}
	if err := c.Delete([]string{"model_05"}, 1, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if c.ByModel("MODEL_05") != nil {
		t.Fatal("delete not reloaded")
	}
	c.Close()
	c, err = Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if len(c.Entries(true)) != 9 || c.ByModel("MODEL_05") != nil {
		t.Fatal("restart differs")
	}
	if err := c.Delete([]string{"absent"}, 0, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	backups, _ = filepath.Glob(filepath.Join(directory, "tonies.custom.json.*.bak"))
	if len(backups) != 0 {
		t.Fatal("retention zero retained backups", backups)
	}
}
func TestDuplicatePairsDoNotPublish(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	one, two := customEntry("ONE"), customEntry("TWO")
	for _, entry := range []Entry{one, two} {
		put(entry, "audio_id", []any{123})
		put(entry, "hash", []string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"})
	}
	if err := c.Upsert([]Entry{one, two}, 0, time.Now()); err == nil {
		t.Fatal("duplicate pair accepted")
	}
	if len(c.Entries(true)) != 0 {
		t.Fatal("invalid entries published")
	}
}

func TestRenameJournalBlocksMutationAndReplaysCompletedCatalog(t *testing.T) {
	directory := t.TempDir()
	c, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	if err := c.Upsert([]Entry{customEntry("OLD")}, 2, now); err != nil {
		t.Fatal(err)
	}
	if err := c.Rename("OLD", "NEW", "content-root", true, 2, now.Add(time.Second), func(string, string) error { return fmt.Errorf("injected interruption") }); err == nil {
		t.Fatal("interruption ignored")
	}
	if err := c.Upsert([]Entry{customEntry("OTHER")}, 2, now); err == nil {
		t.Fatal("mutation accepted with pending intent")
	}
	if err := c.RecoverRename("different-root", 2, now, func(string, string) error { return nil }); err == nil {
		t.Fatal("changed content root accepted")
	}
	if err := c.RecoverRename("content-root", 2, now.Add(2*time.Second), func(string, string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if c.ByModel("NEW") == nil || c.ByModel("OLD") != nil {
		t.Fatal("recovery did not publish new catalog")
	}
	// Simulate interruption after catalog publication and before intent removal.
	data, _ := json.Marshal(renameIntent{From: "OLD", To: "NEW", ContentRoot: "content-root", Linked: true})
	if err := c.root.WriteAtomic(RenameJournal, data, 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := filepath.Glob(filepath.Join(directory, "tonies.custom.json.*.bak"))
	called := 0
	if err := c.RecoverRename("content-root", 2, now.Add(3*time.Second), func(string, string) error { called++; return nil }); err != nil {
		t.Fatal(err)
	}
	after, _ := filepath.Glob(filepath.Join(directory, "tonies.custom.json.*.bak"))
	if called != 1 || len(before) != len(after) {
		t.Fatal("completed catalog recovery was not idempotent")
	}
}
