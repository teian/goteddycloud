// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goteddycloud/internal/content"
)

type metadataTransport func(*http.Request) (*http.Response, error)

func (f metadataTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBundledCatalogRecognizesTagsWhenUpdatesAreOffline(t *testing.T) {
	bundle := t.TempDir()
	for name, data := range map[string]string{
		"tonies.json":     recognitionCatalog,
		"tonieboxes.json": `[{"id":"baseline-box","name":"Baseline box","img_src":"/img_unknown.png"}]`,
	} {
		if err := os.WriteFile(filepath.Join(bundle, name), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GOTEDDYCLOUD_BUNDLED_CATALOG", bundle)
	s := fixtureApp(t)
	s.MetadataHTTP = &http.Client{Transport: metadataTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline")
	})}
	if err := s.updateCatalog(context.Background()); err == nil {
		t.Fatal("offline download unexpectedly succeeded")
	}
	if err := s.updateBoxCatalog(context.Background()); err == nil {
		t.Fatal("offline box catalog download unexpectedly succeeded")
	}
	snap := s.Settings.Snapshot("")
	const ruid = "16000000500304e0"
	if err := s.Content.Update(snap, ruid, func(m content.Metadata) error {
		m.Set("box_audio_id", uint32(0x60000011))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	info, err := s.tagInfo(context.Background(), snap, "", ruid)
	if err != nil || info["tonieInfo"].(map[string]any)["model"] != "AUTO_TEST" {
		t.Fatal("baseline recognition failed", info, err)
	}
	if reply := perform(s, "GET", "/api/toniesJson", nil, false); reply.Code != 200 || reply.Body.String() != recognitionCatalog {
		t.Fatal("baseline catalog unavailable to frontend", reply.Code)
	}
	if reply := perform(s, "GET", "/api/tonieboxesJson", nil, false); reply.Code != 200 || !bytes.Contains(reply.Body.Bytes(), []byte("baseline-box")) {
		t.Fatal("baseline box catalog unavailable to frontend", reply.Code)
	}
}

func TestMetadataCatalogMirrorRedirectPolicy(t *testing.T) {
	client := metadataClient(nil)
	const origin = "https://api.revvox.de/tonies.json?source=teddyCloud&version=goteddycloud"
	const mirror = "https://raw.githubusercontent.com/toniebox-reverse-engineering/tonies-json/release/"
	for _, test := range []struct {
		name, from, to string
		allowed        bool
	}{
		{"official catalog mirror", origin, mirror + "tonies.json", true},
		{"box catalog mirror", "https://api.revvox.de/tonieboxes.json", mirror + "tonieboxes.json", true},
		{"same host", origin, "https://api.revvox.de/tonies.json?updated=1", true},
		{"different mirror file", origin, mirror + "tonieboxes.json", false},
		{"different repository", origin, "https://raw.githubusercontent.com/other/repository/release/tonies.json", false},
		{"untrusted target", origin, "https://untrusted.invalid/tonies.json", false},
		{"private target", origin, "https://127.0.0.1/tonies.json", false},
		{"TLS downgrade", origin, "http://api.revvox.de/tonies.json", false},
		{"credentials", origin, "https://user:password@api.revvox.de/tonies.json", false},
		{"unrelated source", "https://images.invalid/tonies.json", mirror + "tonies.json", false},
		{"unrelated metadata path", "https://api.revvox.de/image.png", mirror + "image.png", false},
		{"mirror query", origin, mirror + "tonies.json?other=1", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			from, _ := http.NewRequest("GET", test.from, nil)
			to, _ := http.NewRequest("GET", test.to, nil)
			err := client.CheckRedirect(to, []*http.Request{from})
			if (err == nil) != test.allowed {
				t.Fatalf("allowed=%v, error=%v", test.allowed, err)
			}
			if err := client.CheckRedirect(to, []*http.Request{from, from, from, from, from}); err != http.ErrUseLastResponse {
				t.Fatal("redirect limit not enforced", err)
			}
		})
	}
}

func TestMetadataUpdatePublishesValidatedRawBytesAndRetainsFailures(t *testing.T) {
	s := fixtureApp(t)
	root := s.Settings.Snapshot("").String("internal.configdirfull")
	before, err := s.Catalog.ReadOfficial()
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("[\n {\"model\":\"PUBLIC_NEW\",\"title\":\"New\",\"unknown\":42}\n]\n")
	status := 200
	length := int64(len(payload))
	fail := false
	s.MetadataHTTP = &http.Client{Transport: metadataTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://api.revvox.de/tonies.json?source=teddyCloud&version=goteddycloud" || r.Header.Get("Authorization") != "" || r.Method != "GET" || r.Header.Get("User-Agent") != "teddyCloud/goteddycloud" {
			t.Errorf("unexpected metadata request %v", r)
		}
		if fail {
			return nil, errors.New("private credentials must not escape")
		}
		return &http.Response{StatusCode: status, ContentLength: length, Body: io.NopCloser(bytes.NewReader(payload)), Header: make(http.Header)}, nil
	})}
	assertUnchanged := func(expected []byte) {
		t.Helper()
		got, err := os.ReadFile(filepath.Join(root, "tonies.json"))
		if err != nil || !bytes.Equal(got, expected) {
			t.Fatalf("disk changed: %q %v", got, err)
		}
	}
	status = 503
	if err := s.updateCatalog(context.Background()); err == nil || err.Error() != "metadata source rejected request: HTTP 503" {
		t.Fatal("missing failed status", err)
	}
	assertUnchanged(before)
	status = 200
	length++
	if err := s.updateCatalog(context.Background()); err == nil {
		t.Fatal("accepted short transfer")
	}
	assertUnchanged(before)
	length = int64(len(payload))
	if err := s.updateCatalog(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertUnchanged(payload)
	if s.Catalog.ByModel("PUBLIC_NEW").String("title") != "New" {
		t.Fatal("active lookup stale")
	}
	if w := perform(s, "GET", "/api/toniesJson", nil, false); w.Code != 200 || !bytes.Equal(w.Body.Bytes(), payload) {
		t.Fatal("raw bytes changed", w.Code, w.Body.String())
	}
	valid := payload
	payload = []byte(`[{"model":`)
	length = int64(len(payload))
	if err := s.updateCatalog(context.Background()); err == nil {
		t.Fatal("accepted malformed document")
	}
	assertUnchanged(valid)
	if s.Catalog.ByModel("PUBLIC_NEW") == nil {
		t.Fatal("failed download destroyed active lookup")
	}
	fail = true
	if err := s.updateCatalog(context.Background()); err == nil || err.Error() != "metadata source unavailable" {
		t.Fatal(err)
	}
	assertUnchanged(valid)
	if w := perform(s, "GET", "/api/toniesJsonUpdate", nil, false); w.Code != 200 || w.Body.String() != "Triggered tonies.json update" || !w.Flushed {
		t.Fatal(w.Code, w.Body.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.updateCatalog(ctx); err == nil {
		t.Fatal("accepted canceled update")
	}
	s.metadataUpdate <- struct{}{}
	if err := s.updateCatalog(context.Background()); err == nil || err.Error() != "metadata update already in progress" {
		t.Fatal(err)
	}
	<-s.metadataUpdate
}

func TestMetadataReadMissingAndInvalidReloadRetainsLookup(t *testing.T) {
	s := fixtureApp(t)
	original := s.Catalog.Entries(false)
	path := filepath.Join(s.Settings.Snapshot("").String("internal.configdirfull"), "tonies.json")
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if w := perform(s, "GET", "/api/toniesJsonReload", nil, false); w.Code != 500 {
		t.Fatal(w.Code)
	}
	if len(s.Catalog.Entries(false)) != len(original) {
		t.Fatal("reload replaced valid snapshot")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if w := perform(s, "GET", "/api/toniesJson", nil, false); w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestStartupCatalogUpdatesAndCORS(t *testing.T) {
	s := fixtureApp(t)
	if err := s.Settings.Set("", "core.tonies_json_auto_update", "true"); err != nil {
		t.Fatal(err)
	}
	if err := s.Settings.Set("", "core.allowOrigin", "https://frontend.synthetic.invalid"); err != nil {
		t.Fatal(err)
	}
	calls := make(chan string, 2)
	s.MetadataHTTP = &http.Client{Transport: metadataTransport(func(r *http.Request) (*http.Response, error) {
		calls <- r.URL.Path
		if r.Header.Get("Authorization") != "" || r.Header.Get("User-Agent") != "teddyCloud/goteddycloud" {
			t.Error("unexpected metadata headers")
		}
		payload := []byte(`[{"model":"STARTUP","unknown":42}]`)
		return &http.Response{StatusCode: 200, ContentLength: int64(len(payload)), Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(payload))}, nil
	})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.StartIntegrations(ctx, "synthetic"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/tonies.json", "/tonieboxes.json"} {
		select {
		case got := <-calls:
			if got != want {
				t.Fatal(got, want)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("startup update timeout")
		}
	}
	// Receiving the second request does not mean its response has been saved.
	// Keep the integration context alive until the update is published.
	boxPath := filepath.Join(s.Settings.Snapshot("").String("internal.configdirfull"), "tonieboxes.json")
	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile(boxPath)
		if err == nil && bytes.Contains(data, []byte("STARTUP")) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("startup box catalog was not published", err)
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	s.integrations.Wait()
	if s.Catalog.ByModel("STARTUP") == nil {
		t.Fatal("startup catalog missing")
	}
	boxData, err := os.ReadFile(boxPath)
	if err != nil || !bytes.Contains(boxData, []byte("STARTUP")) {
		t.Fatal(string(boxData), err)
	}
	for _, box := range []bool{false, true} {
		w := perform(s, "GET", "/v1/time", nil, box)
		if w.Header().Get("Access-Control-Allow-Origin") != "https://frontend.synthetic.invalid" {
			t.Fatal("missing CORS", box)
		}
	}
}
