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
)

type metadataTransport func(*http.Request) (*http.Response, error)

func (f metadataTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

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
		if r.URL.String() != "https://api.revvox.de/tonies.json?source=teddyCloud&version=goteddycloud" || r.Header.Get("Authorization") != "" || r.Method != "GET" {
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
	if err := s.updateCatalog(context.Background()); err == nil {
		t.Fatal("accepted failed status")
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
		if r.Header.Get("Authorization") != "" {
			t.Error("metadata received credentials")
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
	cancel()
	s.integrations.Wait()
	if s.Catalog.ByModel("STARTUP") == nil {
		t.Fatal("startup catalog missing")
	}
	boxData, err := os.ReadFile(filepath.Join(s.Settings.Snapshot("").String("internal.configdirfull"), "tonieboxes.json"))
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
