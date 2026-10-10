// SPDX-License-Identifier: GPL-2.0-only
package content

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"goteddycloud/internal/config"
	"goteddycloud/internal/fixtures"
)

func TestPlaylistGenerationCacheAndFailurePreservation(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg unavailable")
	}
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
	library := snap.String("internal.librarydirfull")
	for _, name := range []string{"tone1.wav", "tone2.wav"} {
		data, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(library, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	const ruid = "0000000100000001"
	playlist := Playlist{Type: "tap", AudioID: 123456, Filepath: "lib://playlist-cache.taf", Name: "Synthetic", Files: []PlaylistFile{{Filepath: "lib://tone1.wav", Name: "One"}, {Filepath: "lib://tone2.wav", Name: "Two"}}}
	save := func() {
		t.Helper()
		data, err := json.Marshal(playlist)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(library, "playlist.tap"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	save()
	if err := store.Update(snap, ruid, func(m Metadata) error { m.Set("source", "lib://playlist.tap"); return nil }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.EnsurePlaylist(ctx, snap, snap, ruid); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	file, header, err := store.Open(snap, snap, ruid)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if header.GetAudioId() != 123456 || len(header.TrackPageNums) != 2 {
		t.Fatal(header)
	}
	output := filepath.Join(library, "playlist-cache.taf")
	before, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsurePlaylist(ctx, snap, snap, ruid); err != nil {
		t.Fatal(err)
	}
	afterInfo, _ := os.Stat(output)
	if !afterInfo.ModTime().Equal(info.ModTime()) {
		t.Fatal("cache unnecessarily regenerated")
	}
	playlist.AudioID++
	playlist.Files[1].Filepath = "lib://missing.wav"
	save()
	if err := store.EnsurePlaylist(ctx, snap, snap, ruid); err == nil {
		t.Fatal("missing track accepted")
	}
	after, _ := os.ReadFile(output)
	if !bytes.Equal(before, after) {
		t.Fatal("failed generation destroyed cache")
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if err := store.EnsurePlaylist(canceled, snap, snap, ruid); err == nil {
		t.Fatal("canceled conversion accepted")
	}
	playlist.Files[1].Filepath = "lib://../../outside.wav"
	save()
	if err := store.EnsurePlaylist(ctx, snap, snap, ruid); err == nil {
		t.Fatal("path escape accepted")
	}
	outside := filepath.Join(t.TempDir(), "private.wav")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(library, "outside.wav")); err != nil {
		t.Skip(err)
	}
	playlist.Files[1].Filepath = "lib://outside.wav"
	save()
	if err := store.EnsurePlaylist(ctx, snap, snap, ruid); err == nil {
		t.Fatal("symlink escape accepted")
	}
	after, _ = os.ReadFile(output)
	if !bytes.Equal(before, after) {
		t.Fatal("escape attempt destroyed cache")
	}
	playlist.Files[1].Filepath = "lib://tone2.wav"
	save()
	if err := store.EnsurePlaylist(ctx, snap, snap, ruid); err != nil {
		t.Fatal(err)
	}
	file, header, err = store.Open(snap, snap, ruid)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if header.GetAudioId() != playlist.AudioID {
		t.Fatal("identity change not regenerated")
	}
}
