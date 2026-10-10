// SPDX-License-Identifier: GPL-2.0-only
package config

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestDefaultsOverlayPersistenceAndInvalidUpdates(t *testing.T) {
	base := t.TempDir()
	s, err := Open(base)
	if err != nil {
		t.Fatal(err)
	}
	if s.Snapshot("").Uint("toniebox.max_vol_spk") != 3 {
		t.Fatal("wrong default")
	}
	if err := s.Set("001122334455", "toniebox.max_vol_spk", "1"); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(base, "config/config.overlay.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("001122334455", "toniebox.max_vol_spk", "5"); err == nil {
		t.Fatal("out of range accepted")
	}
	after, _ := os.ReadFile(filepath.Join(base, "config/config.overlay.ini"))
	if string(before) != string(after) {
		t.Fatal("invalid update changed disk")
	}
	if s.Snapshot("AABBCCDDEEFF").Uint("toniebox.max_vol_spk") != 3 {
		t.Fatal("override leaked")
	}
	s.Close()
	s, err = Open(base)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Snapshot("001122334455").Uint("toniebox.max_vol_spk") != 1 {
		t.Fatal("override lost on restart")
	}
	var wg sync.WaitGroup
	for _, id := range []string{"box_a", "box_b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				if err := s.Set(id, "toniebox.led", "2"); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
}
func TestUnknownFieldsAndPathResolution(t *testing.T) {
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	data := "future.setting=keep=equals\ncore.contentdir=default\ntoniebox.max_vol_spk=999\n"
	if err := os.WriteFile(filepath.Join(base, "config/config.ini"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(base)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v := s.Snapshot("")
	if v.Uint("toniebox.max_vol_spk") != 3 {
		t.Fatal("invalid disk value applied")
	}
	if v.String("internal.contentdirfull") != filepath.Join(base, "data/content/default") {
		t.Fatal(v.String("internal.contentdirfull"))
	}
	if err := s.Set("", "toniebox.led", "1"); err != nil {
		t.Fatal(err)
	}
	dataRead, _ := os.ReadFile(filepath.Join(base, "config/config.ini"))
	if !contains(string(dataRead), "future.setting=keep=equals\n") {
		t.Fatal("unknown setting lost")
	}
}
func contains(s, part string) bool {
	for i := 0; i+len(part) <= len(s); i++ {
		if s[i:i+len(part)] == part {
			return true
		}
	}
	return false
}

func TestReadOnlyOpenDoesNotCreateOrMutate(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "absent")
	if store, err := OpenReadOnly(absent); err == nil {
		store.Close()
		t.Fatal("missing deployment accepted")
	}
	if _, err := os.Stat(absent); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only open created a directory")
	}
	base := t.TempDir()
	writable, err := Open(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := writable.Set("", "cloud.enabled", "false"); err != nil {
		t.Fatal(err)
	}
	writable.Close()
	before, err := os.ReadFile(filepath.Join(base, "config/config.ini"))
	if err != nil {
		t.Fatal(err)
	}
	readonly, err := OpenReadOnly(base)
	if err != nil {
		t.Fatal(err)
	}
	defer readonly.Close()
	if err := readonly.Set("", "cloud.enabled", "true"); err == nil {
		t.Fatal("read-only mutation allowed")
	}
	if err := readonly.RemoveOverlay("one"); err == nil {
		t.Fatal("read-only removal allowed")
	}
	if err := readonly.Validate(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(base, "config/config.ini"))
	if string(before) != string(after) {
		t.Fatal("configuration modified")
	}
}
