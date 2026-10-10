// SPDX-License-Identifier: GPL-2.0-only
package content

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"goteddycloud/internal/config"
	"goteddycloud/internal/fixtures"
	"goteddycloud/internal/taf"
)

func TestMetadataUnknownFieldsConcurrentUpdatesAndSourceConfinement(t *testing.T) {
	base := filepath.Join(t.TempDir(), "fixture")
	if err := fixtures.Generate(base); err != nil {
		t.Fatal(err)
	}
	settings, err := config.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	snap := settings.Snapshot("")
	store := New()
	defer store.Close()
	const ruid = "0000000100000001"
	var wg sync.WaitGroup
	for _, key := range []string{"claimed", "hide"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.Update(snap, ruid, func(m Metadata) error { m.Set(key, true); return nil }); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	m, err := store.Metadata(snap, ruid)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Bool("hide") || !m.Bool("claimed") || m.String("fixture_extension") != "preserve-me" {
		t.Fatal(m)
	}
	if RUID(0x0100000001000000) != ruid {
		t.Fatal("byte order")
	}
	output := filepath.Join(snap.String("internal.librarydirfull"), "synthetic.taf")
	file, err := os.Create(output)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := taf.NewWriter(file, 123456)
	if err != nil {
		t.Fatal(err)
	}
	head := []byte{'O', 'p', 'u', 's', 'H', 'e', 'a', 'd', 1, 2, 0x38, 1, 0x80, 0xbb, 0, 0, 0, 0, 0}
	if err := writer.StartTrack(head); err != nil {
		t.Fatal(err)
	}
	if err := writer.Packet([]byte{0xfb, 3, 0xff, 0xfe, 0xff, 0xfe, 0xff, 0xfe}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	file.Close()
	audio, h, err := store.Open(snap, snap, ruid)
	if err != nil {
		t.Fatal(err)
	}
	audio.Close()
	if h.GetAudioId() != 123456 {
		t.Fatal(h)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "outside.taf"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(snap.String("internal.librarydirfull"), "escape")); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"lib://../outside.taf", "lib://escape/outside.taf", filepath.Join(outside, "outside.taf")} {
		if err := store.Update(snap, ruid, func(m Metadata) error { m.Set("source", source); return nil }); err != nil {
			t.Fatal(err)
		}
		if f, _, err := store.Open(snap, snap, ruid); err == nil {
			f.Close()
			t.Fatalf("escaped through %s", source)
		}
	}
	for _, ruid := range []string{"../outside", "ZZ00000000000000", "000000000000000/"} {
		if _, err := UIDPath(ruid); err == nil {
			t.Fatal(ruid)
		}
	}
	data, err := os.ReadFile(filepath.Join(snap.String("internal.contentdirfull"), "00000001/00000001.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw["fixture_extension"], []byte("preserve-me")) {
		t.Fatal("extension lost")
	}
}
