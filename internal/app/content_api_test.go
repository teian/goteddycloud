// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestContentMetadataEditsPreserveExtensionsAndDownloadConfinesPaths(t *testing.T) {
	s := fixtureApp(t)
	snap := s.Settings.Snapshot("")
	result := perform(s, "POST", "/content/json/set/0000000100000001", []byte("hide=true&live=TRUE&tonie_model=test&cloud_auth=bad&extension=bad"), false)
	if result.Code != 200 || result.Body.String() != "OK" {
		t.Fatal(result.Code, result.Body.String())
	}
	m, err := s.Content.Metadata(snap, "0000000100000001")
	if err != nil || !m.Bool("hide") || m.Bool("live") || m.String("tonie_model") != "test" || m.String("fixture_extension") != "preserve-me" || m.String("cloud_auth") == "bad" {
		t.Fatal(m, err)
	}
	result = perform(s, "GET", "/content/json/get/0000000100000001", nil, false)
	var decoded map[string]any
	if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &decoded) != nil || decoded["fixture_extension"] != "preserve-me" {
		t.Fatal(result.Code, result.Body.String())
	}
	for _, path := range []string{"/content/../config/config.ini", "/content/json/../config.ini", "/content/json/get/not-a-uid", "/content/download/00000001/../config.ini"} {
		result = perform(s, "GET", path, nil, false)
		if result.Code < 400 {
			t.Fatal("unsafe path accepted", path, result.Code)
		}
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(snap.String("internal.librarydirfull"), "outside")); err != nil {
		t.Fatal(err)
	}
	result = perform(s, "GET", "/content/outside/secret?special=library", nil, false)
	if result.Code < 400 {
		t.Fatal("symlink escaped", result.Code)
	}
}

func TestFileUploadCancellationFilenameAndIndexContract(t *testing.T) {
	s := fixtureApp(t)
	snap := s.Settings.Snapshot("")
	library := snap.String("internal.librarydirfull")
	result := multipartAudio(t, s, "/api/fileUpload?special=library", "file.txt", []byte("old"), context.Background())
	if result.Code != 200 {
		t.Fatal(result.Code, result.Body.String())
	}
	for _, name := range []string{"../escape", "other/escape", "..", "\\escape"} {
		result = multipartAudio(t, s, "/api/fileUpload?special=library", name, []byte("bad"), context.Background())
		if result.Code != 400 {
			t.Fatal(name, result.Code)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	multipartAudio(t, s, "/api/fileUpload?special=library", "file.txt", []byte("replacement"), ctx)
	data, _ := os.ReadFile(filepath.Join(library, "file.txt"))
	if string(data) != "old" {
		t.Fatal("canceled upload replaced file", string(data))
	}
	request := httptest.NewRequest("GET", "/api/fileIndexV2?special=library", nil)
	result = httptest.NewRecorder()
	s.Management(result, request)
	var index struct {
		Files []map[string]any `json:"files"`
	}
	if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &index) != nil {
		t.Fatal(result.Code, result.Body.String())
	}
	found := false
	for _, row := range index.Files {
		if row["name"] == "file.txt" {
			found = true
			if row["isDir"] != false || row["hide"] != false || row["size"] != float64(3) {
				t.Fatal(row)
			}
		}
	}
	if !found {
		t.Fatal("uploaded file missing")
	}
	result = perform(s, "GET", "/api/fileIndex?special=library&path=absent", nil, false)
	if result.Code != 200 || result.Body.String() != "{\"files\":[]}" {
		t.Fatal(result.Code, result.Body.String())
	}
	result = perform(s, "GET", "/api/fileIndexV2?special=library&path=../config", nil, false)
	if result.Code != 400 {
		t.Fatal(result.Code)
	}
}
