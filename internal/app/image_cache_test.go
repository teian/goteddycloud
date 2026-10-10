// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestImageCacheDownloadFlushAndBoundedPublication(t *testing.T) {
	s := fixtureApp(t)
	_ = s.Settings.Set("", "core.allowPrivateFetch", "true")
	if err := s.Settings.Set("", "tonie_json.cache_images", "true"); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/ok.png":
			w.Write([]byte("image"))
		case "/missing.png":
			w.WriteHeader(404)
		case "/fail.png":
			w.WriteHeader(503)
		case "/large.png":
			w.Write([]byte(strings.Repeat("x", int(imageLimit+1))))
		}
	}))
	defer remote.Close()
	url := s.imageURL(context.Background(), remote.URL+"/ok.png", false)
	if !strings.HasPrefix(url, "/cache/") || calls.Load() != 0 {
		t.Fatal(url, calls.Load())
	}
	for range 2 {
		result := perform(s, "GET", url, nil, false)
		if result.Code != 200 || result.Body.String() != "image" || result.Header().Get("Content-Type") != "image/png" {
			t.Fatal(result.Code, result.Body.String())
		}
	}
	if calls.Load() != 1 {
		t.Fatal("cache missed", calls.Load())
	}
	stats := s.imageStats()
	if stats["total_entries"] != 2 || stats["exists_entries"] != 1 || stats["total_size"] != 5 {
		t.Fatal(stats)
	}
	result := perform(s, "POST", "/api/cacheFlush", nil, false)
	if result.Code != 200 || !strings.Contains(result.Body.String(), "\"deleted_files\": 1") {
		t.Fatal(result.Code, result.Body.String())
	}
	result = perform(s, "GET", url, nil, false)
	if result.Code != 200 || calls.Load() != 2 {
		t.Fatal(result.Code, calls.Load())
	}
	for _, tc := range []struct {
		name string
		code int
	}{{"missing", 404}, {"fail", 301}, {"large", 301}} {
		url = s.imageURL(context.Background(), remote.URL+"/"+tc.name+".png", false)
		result = perform(s, "GET", url, nil, false)
		if result.Code != tc.code {
			t.Fatal(tc.name, result.Code)
		}
		path := filepath.Join(s.cacheRoot.Name(), strings.TrimPrefix(url, "/cache/"))
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("failed download published", tc.name, err)
		}
	}
	if result := perform(s, "GET", "/cache/unregistered.png", nil, false); result.Code != 404 {
		t.Fatal(result.Code)
	}
}
