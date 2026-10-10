// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"goteddycloud/internal/config"
)

type otaCloud struct {
	data            []byte
	length          int64
	filename, query string
	authorization   string
	calls           int
	omitDisposition bool
}

func (c *otaCloud) Do(_ context.Context, _ config.Snapshot, _ config.Snapshot, _, _, query string, headers http.Header, _ []byte) (*http.Response, error) {
	c.query = query
	c.authorization = headers.Get("Authorization")
	c.calls++
	responseHeaders := http.Header{}
	if !c.omitDisposition {
		responseHeaders.Set("Content-Disposition", "attachment;filename="+c.filename)
	}
	return &http.Response{StatusCode: 200, Header: responseHeaders, ContentLength: c.length, Body: io.NopCloser(bytes.NewReader(c.data))}, nil
}
func TestOTACacheSelectionAndInterruptedDownload(t *testing.T) {
	s := fixtureApp(t)
	for _, key := range []string{"cloud.enabled", "cloud.enableV1Ota", "cloud.cacheOta", "cloud.localOta"} {
		if err := s.Settings.Set("", key, "true"); err != nil {
			t.Fatal(err)
		}
	}
	mock := &otaCloud{data: []byte("synthetic firmware"), length: 18, filename: "300-test.bin"}
	s.Cloud = mock
	reply := perform(s, "GET", "/v1/ota/2?cv=0", nil, true)
	if reply.Code != 304 || mock.query != "cv=1" {
		t.Fatal(reply.Code, mock.query)
	}
	path := filepath.Join(s.Settings.Snapshot("").String("internal.firmwaredirfull"), "ota/2/300-test.bin")
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "synthetic firmware" {
		t.Fatal(string(data), err)
	}
	reply = perform(s, "GET", "/v1/ota/2?cv=0", nil, true)
	if reply.Code != 200 || reply.Body.String() != "synthetic firmware" || mock.query != "cv=300" {
		t.Fatal(reply.Code, mock.query)
	}
	mock.filename = "400-test.bin"
	mock.data = []byte("short")
	reply = perform(s, "GET", "/v1/ota/2?cv=0", nil, true)
	if reply.Code != 200 || reply.Body.String() != "synthetic firmware" {
		t.Fatal("interrupted fetch lost old firmware")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "400-test.bin")); !os.IsNotExist(err) {
		t.Fatal("partial firmware published", err)
	}
	if mock.authorization != "" {
		t.Fatal("tag token used for OTA")
	}
}
func TestOTADeviceFoldersAndUserAgentState(t *testing.T) {
	s := fixtureApp(t)
	id := "001122334455"
	before, _ := os.ReadFile(filepath.Join(s.Settings.Snapshot("").Base, "config/config.overlay.ini"))
	for _, tc := range []struct {
		ua               string
		chip, generation uint64
	}{
		{"TB/1700000000 SP/123 HW/1000000", 1, 1},
		{"TB/1700000000 SP/123 HW/1200000", 2, 1},
		{"red TB/1700000000", 3, 1},
		{"toniebox-esp32-eu/v5.226.0", 3, 1},
		{"TB2/1.0.22-92f57d4", 4, 2},
		{"unknown-device", 0, 0},
	} {
		s.boxUserAgent(id, tc.ua)
		snap := s.Settings.Snapshot(id)
		if snap.Uint("internal.toniebox_firmware.boxIC") != tc.chip || snap.Uint("toniebox.boxGeneration") != tc.generation {
			t.Fatal(tc, snap.Values)
		}
		directory := otaDirectory(snap, 3)
		if tc.chip == 1 && directory != filepath.Join("ota", "cc3200", "3") {
			t.Fatal(directory)
		}
	}
	after, _ := os.ReadFile(filepath.Join(s.Settings.Snapshot("").Base, "config/config.overlay.ini"))
	if !bytes.Equal(before, after) {
		t.Fatal("derived box state persisted")
	}
	if s.Settings.Snapshot("AABBCCDDEEFF").Uint("internal.toniebox_firmware.boxIC") != 0 {
		t.Fatal("box model leaked")
	}
}

func TestV3OTACacheWithoutDispositionAndInterruptedDownload(t *testing.T) {
	s := fixtureApp(t)
	for _, key := range []string{"cloud.enabled", "cloud.enableV3Ota", "cloud.cacheOta", "cloud.localOta"} {
		if err := s.Settings.Set("", key, "true"); err != nil {
			t.Fatal(err)
		}
	}
	mock := &otaCloud{data: []byte("synthetic firmware"), length: 18, omitDisposition: true}
	s.Cloud = mock
	reply := perform(s, "GET", "/v3/ota/2/a1b2c3d4", nil, true)
	if reply.Code != 200 || reply.Body.String() != "synthetic firmware" {
		t.Fatal(reply.Code, reply.Body.String())
	}
	path := filepath.Join(s.Settings.Snapshot("").String("internal.firmwaredirfull"), "ota/2/a1b2c3d4.bin")
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "synthetic firmware" {
		t.Fatal(err, string(data))
	}
	mock.data = []byte("partial")
	reply = perform(s, "GET", "/v3/ota/2/b2c3d4e5", nil, true)
	if reply.Code != 404 {
		t.Fatal(reply.Code)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "b2c3d4e5.bin")); !os.IsNotExist(err) {
		t.Fatal("partial V3 OTA published", err)
	}
	if mock.authorization != "" {
		t.Fatal("tag auth leaked to V3 OTA")
	}
}

func TestDevelopOTARejectsNonHexHash(t *testing.T) {
	s := fixtureApp(t)
	for _, hash := range []string{"synthetic-hash", "g123", "abc..", "../key", "abc/def", ""} {
		w := perform(s, "GET", "/v3/ota/3/"+hash, nil, true)
		if w.Code != 400 {
			t.Fatal(hash, w.Code)
		}
	}
}
