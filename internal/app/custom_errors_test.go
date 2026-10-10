// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goteddycloud/internal/catalog"
)

func TestCustomMutationErrorsPreserveReferenceResponses(t *testing.T) {
	directory := t.TempDir()
	store, err := catalog.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	missing := store.Rename("missing", "new", directory, false, 0, time.Now(), nil)
	invalid := store.Upsert([]catalog.Entry{nil}, 0, time.Now())
	if err := os.WriteFile(filepath.Join(directory, catalog.RenameJournal), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	pending := store.Delete([]string{"missing"}, 0, time.Now())
	if err := os.Remove(filepath.Join(directory, catalog.RenameJournal)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "tonies.custom.json"), []byte("invalid JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	load := store.Delete([]string{"missing"}, 0, time.Now())
	for _, test := range []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"missing", missing, 404, "Source model not found"},
		{"invalid", invalid, 400, "Invalid payload: each entry must be object"},
		{"pending", pending, 500, "Pending custom model rename must be recovered"},
		{"load", load, 500, "Failed to load tonies.custom.json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.err == nil {
				t.Fatal("expected failure")
			}
			// Classification survives wrapping and no longer relies on error prose.
			status, message := customMutationResponse(fmt.Errorf("operation failed: %w", test.err))
			if status != test.status || message != test.message {
				t.Fatal(status, message)
			}
		})
	}
}
