// SPDX-License-Identifier: GPL-2.0-only
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSnapshotCacheInvalidationAndIsolation(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ids := []string{"", "001122334455", "AABBCCDDEEFF", "unknown"}
	check := func() {
		t.Helper()
		for _, id := range ids {
			actual := store.Snapshot(id)
			store.stateMu.Lock()
			expected := store.buildSnapshotLocked(id)
			store.stateMu.Unlock()
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("stale or incorrect snapshot for %q", id)
			}
			if store.String(id, "internal.librarydirfull") != expected.String("internal.librarydirfull") {
				t.Fatal("single-setting read disagrees")
			}
			actual.Values["core.librarydir"] = "caller changed this"
			if store.Snapshot(id).String("core.librarydir") == "caller changed this" {
				t.Fatal("caller mutated cached state")
			}
		}
	}
	apply := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		check()
	}
	check()
	apply(store.Set("", "core.librarydir", "new-library"))
	apply(store.Set(ids[1], "core.librarydir", "box-library"))
	apply(store.Set(ids[2], "toniebox.max_vol_spk", "1"))
	apply(store.SetRuntime("", map[string]string{"internal.security_mit.incident": "true"}))
	apply(store.SetRuntime(ids[1], map[string]string{"internal.security_mit.incident": "false"}))
	apply(store.SetRuntime("unknown", map[string]string{"internal.security_mit.incident": "false"}))
	apply(store.Reset(ids[1], "core.librarydir"))
	if err := store.Set(ids[1], "toniebox.max_vol_spk", "500"); err == nil {
		t.Fatal("invalid update accepted")
	}
	check()
	apply(store.RemoveOverlay(ids[2]))
	if err := os.WriteFile(filepath.Join(store.base, "config/config.ini"), []byte("core.librarydir=reloaded\n"), 0600); err != nil {
		t.Fatal(err)
	}
	apply(store.Reload())
	if err := os.WriteFile(filepath.Join(store.base, "config/config.ini"), []byte("toniebox.max_vol_spk=500\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Reload(); err == nil {
		t.Fatal("invalid reload accepted")
	}
	check()
}
func TestUnknownOverlayQueriesDoNotGrowSnapshotCache(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for index := range 1000 {
		snapshot := store.Snapshot(fmt.Sprintf("unknown%d", index))
		if snapshot.String("commonName") == "" {
			t.Fatal("missing common name")
		}
	}
	if len(store.active.Load().cached) != 1 {
		t.Fatalf("retained unknown overlay queries: %d", len(store.active.Load().cached))
	}
}

func TestImmutableViewSurvivesUpdates(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	old := store.View("")
	if err := store.Set("", "toniebox.max_vol_spk", "1"); err != nil {
		t.Fatal(err)
	}
	if old.Uint("toniebox.max_vol_spk") != 3 || store.View("").Uint("toniebox.max_vol_spk") != 1 {
		t.Fatal("view is not an immutable point-in-time read")
	}
	copy := store.Snapshot("")
	copy.Values["toniebox.max_vol_spk"] = "2"
	if store.View("").Uint("toniebox.max_vol_spk") != 1 {
		t.Fatal("mutable snapshot changed view")
	}
}

func TestDefinitionsReturnIndependentSlices(t *testing.T) {
	definitions := Definitions()
	original := definitions[0]
	definitions[0].ID = "caller mutation"
	definitions[0].Default = "caller mutation"
	if got := Definitions()[0]; !reflect.DeepEqual(got, original) {
		t.Fatal("schema definition mutation escaped")
	}
}
