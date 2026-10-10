// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goteddycloud/internal/config"
)

type reverseCloud struct {
	path, query, method, auth string
	fail                      bool
}

func (c *reverseCloud) Do(_ context.Context, _, _ config.Snapshot, method, path, query string, headers http.Header, body []byte) (*http.Response, error) {
	c.path = path
	c.query = query
	c.method = method
	c.auth = headers.Get("Authorization")
	if c.fail {
		return nil, fmt.Errorf("unavailable")
	}
	if len(body) != 0 {
		return nil, fmt.Errorf("unexpected body")
	}
	return &http.Response{StatusCode: 201, Header: http.Header{"Content-Type": {"text/plain"}, "X-Remote": {"yes"}, "Connection": {"X-Private"}, "X-Private": {"secret"}}, Body: io.NopCloser(strings.NewReader("cloud"))}, nil
}
func TestReverseCloudConfiguredForwardingAndAuth(t *testing.T) {
	s := fixtureApp(t)
	if err := s.Settings.Set("", "cloud.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	c := &reverseCloud{}
	s.Cloud = c
	req := httptest.NewRequest("POST", "/reverse/v1/test?one=two", strings.NewReader("ignored"))
	req.Header.Set("Authorization", "BD "+strings.Repeat("ab", 32))
	w := httptest.NewRecorder()
	s.Management(w, req)
	if w.Code != 201 || w.Body.String() != "cloud" || w.Header().Get("X-Remote") != "yes" || w.Header().Get("X-Private") != "" {
		t.Fatal(w)
	}
	if c.path != "/v1/test" || c.query != "one=two" || c.method != "GET" || c.auth != "BD "+strings.Repeat("AB", 32) {
		t.Fatal(c)
	}
	c.fail = true
	if w := perform(s, "GET", "/reverse/v1/test", nil, false); w.Code != 502 {
		t.Fatal(w.Code)
	}
	if s.reverseRequests.Load() != 2 {
		t.Fatal("request counter")
	}
}

func TestReverseGenericTargetsCacheExpiryAndFailure(t *testing.T) {
	s := fixtureApp(t)
	now := time.Unix(1800000000, 0)
	s.Now = func() time.Time { return now }
	calls := 0
	status := 200
	payload := "fresh"
	short := false
	s.MetadataHTTP = &http.Client{Transport: metadataTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.URL.Scheme != "https" || r.Header.Get("User-Agent") != "teddyCloud/goteddycloud" {
			t.Errorf("unexpected forwarding %v", r)
		}
		if calls == 1 && r.URL.String() != "https://api.macvendors.com/AA:BB" {
			t.Errorf("unexpected target %v", r.URL)
		}
		length := int64(len(payload))
		if short {
			length++
		}
		return &http.Response{StatusCode: status, ContentLength: length, Header: http.Header{"Content-Type": {"text/remote"}, "Etag": {"test"}, "Transfer-Encoding": {"chunked"}, "Access-Control-Allow-Origin": {"*"}}, Body: io.NopCloser(strings.NewReader(payload))}, nil
	})}
	if w := perform(s, "POST", "/reverseGeneric/macvendor?AA:BB", []byte("ignored"), false); w.Code != 200 || w.Body.String() != "fresh" || w.Header().Get("Content-Type") != "text/remote" || w.Header().Get("Transfer-Encoding") != "" || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal(w)
	}
	payload = "should not be fetched"
	if w := perform(s, "GET", "/reverseGeneric/macvendor?AA:BB", nil, false); w.Body.String() != "fresh" || w.Header().Get("Content-Type") != "text/plain" || calls != 1 {
		t.Fatal(w, calls)
	}
	now = now.Add(24 * time.Hour)
	if w := perform(s, "GET", "/reverseGeneric/macvendor?AA:BB", nil, false); w.Body.String() != payload || calls != 2 {
		t.Fatal(w, calls)
	}
	// Non-200 responses must never enter the cache.
	status = 429
	payload = "limited"
	for i := 0; i < 2; i++ {
		if w := perform(s, "GET", "/reverseGeneric/macvendor/other", nil, false); w.Code != 429 {
			t.Fatal(w)
		}
	}
	if calls != 4 {
		t.Fatal("error response cached", calls)
	}
	status = 200
	short = true
	if w := perform(s, "GET", "/reverseGeneric/macvendor/short", nil, false); w.Code != 502 {
		t.Fatal(w.Code)
	}
	short = false
	payload = "complete"
	if w := perform(s, "GET", "/reverseGeneric/macvendor/short", nil, false); w.Body.String() != payload {
		t.Fatal("partial download poisoned cache", w)
	}
	for _, path := range []string{"/reverseGeneric", "/reverseGeneric/unknown", "/reverseGeneric/macvendor-extra"} {
		if w := perform(s, "GET", path, nil, false); w.Code != 404 {
			t.Fatal(path, w.Code)
		}
	}
	// A restarted process flushes the first matching release cache even when fresh.
	cache := s.Settings.Snapshot("").String("internal.cachedirfull")
	target := reverseTargets["teddycloud_release"]
	prefix := fmt.Sprintf("reverse.teddycloud_release.%X.", sha256.Sum256([]byte(target.path)))
	file := filepath.Join(cache, prefix+fmt.Sprint(now.Unix()))
	if err := os.WriteFile(file, []byte("old release"), 0600); err != nil {
		t.Fatal(err)
	}
	count := calls
	if w := perform(s, "GET", "/reverseGeneric/teddycloud_release?ignored=1", nil, false); w.Body.String() != payload || calls != count+1 {
		t.Fatal("restart cache retained", w, calls)
	}
	if w := perform(s, "GET", "/reverseGeneric/teddycloud_release", nil, false); w.Body.String() != payload || calls != count+1 {
		t.Fatal("release cache not reused", w, calls)
	}
}

func TestReverseCacheCannotReadSymlinkOutsideRoot(t *testing.T) {
	s := fixtureApp(t)
	s.Now = func() time.Time { return time.Unix(1800000000, 0) }
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	prefix := fmt.Sprintf("reverse.macvendor.%X.", sha256.Sum256([]byte("/test")))
	link := filepath.Join(s.Settings.Snapshot("").String("internal.cachedirfull"), prefix+"1800000000")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip(err)
	}
	s.MetadataHTTP = &http.Client{Transport: metadataTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, ContentLength: 4, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("safe"))}, nil
	})}
	if w := perform(s, "GET", "/reverseGeneric/macvendor/test", nil, false); w.Body.String() != "safe" {
		t.Fatal(w.Body.String())
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "private" {
		t.Fatal("outside changed", err)
	}
}
