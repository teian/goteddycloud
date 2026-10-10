// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"goteddycloud/internal/content"
	"goteddycloud/internal/taf"
)

func TestStoredDeliveryGeneratesConfinedPlaylistBeforeOpeningTAF(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg unavailable")
	}
	s := fixtureApp(t)
	snap := s.Settings.Snapshot("")
	library := snap.String("internal.librarydirfull")
	for _, name := range []string{"tone1.wav", "tone2.wav"} {
		data, err := os.ReadFile(filepath.Join(snap.Base, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(library, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	playlist := content.Playlist{Type: "tap", AudioID: 123, Filepath: "lib://generated.taf", Name: "Synthetic", Files: []content.PlaylistFile{{Filepath: "lib://tone1.wav"}, {Filepath: "lib://tone2.wav"}}}
	data, _ := json.Marshal(playlist)
	if err := os.WriteFile(filepath.Join(library, "playlist.tap"), append(bytes.Repeat([]byte(" "), 512), data...), 0600); err != nil {
		t.Fatal(err)
	}
	const ruid = "0000000100000001"
	if err := s.Content.Update(snap, ruid, func(m content.Metadata) error { m.Set("source", "lib://playlist.tap"); return nil }); err != nil {
		t.Fatal(err)
	}
	result := perform(s, "GET", "/v1/content/"+ruid, nil, true)
	if result.Code != 200 {
		t.Fatal(result.Code, result.Body.String())
	}
	header, err := taf.Validate(bytes.NewReader(result.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if header.GetAudioId() != 123 || len(header.TrackPageNums) != 2 {
		t.Fatal(header)
	}
	metadata := perform(s, "GET", "/v3/content-meta/"+ruid, nil, true)
	var meta struct {
		Version uint32            `json:"version"`
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(metadata.Body.Bytes(), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Version != 123 || len(meta.Content) != 2 {
		t.Fatal(meta)
	}
	chapter := perform(s, "GET", "/v3/chapter/teddycloud_01_"+ruid, nil, true)
	if chapter.Code != 200 || chapter.Body.Len() <= 512 {
		t.Fatal(chapter.Code, chapter.Body.Len())
	}
}
