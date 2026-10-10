// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStaticSPAMIMEAndConfinement(t *testing.T) {
	s := fixtureApp(t)
	www := s.Settings.Snapshot("").String("internal.wwwdirfull")
	if err := os.WriteFile(filepath.Join(www, "web/index.html"), []byte("official fixture entry"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(www, "web/app.js"), []byte("export default 42"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/web", "/web/", "/web/boxes/001122334455"} {
		w := perform(s, "GET", path, nil, false)
		if w.Code != 200 || w.Body.String() != "official fixture entry" || w.Header().Get("Content-Type") != "text/html" {
			t.Fatal(path, w)
		}
	}
	if w := perform(s, "GET", "/web/app.js", nil, false); w.Code != 200 || w.Header().Get("Content-Type") != "application/javascript" {
		t.Fatal(w)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(www, "escape")); err != nil {
		t.Fatal(err)
	}
	if w := perform(s, "GET", "/escape/secret", nil, false); w.Code != 404 {
		t.Fatal("static root escaped")
	}
	if w := perform(s, "GET", "/web", nil, true); w.Code != 404 {
		t.Fatal("static served on box listener")
	}
}

func TestStaticWebAssemblyMIME(t *testing.T) {
	s := fixtureApp(t)
	module := []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}
	www := s.Settings.Snapshot("").String("internal.wwwdirfull")
	if err := os.WriteFile(filepath.Join(www, "web/encoder.wasm"), module, 0600); err != nil {
		t.Fatal(err)
	}
	reply := perform(s, "GET", "/web/encoder.wasm", nil, false)
	if reply.Code != 200 || reply.Header().Get("Content-Type") != "application/wasm" || !bytes.Equal(reply.Body.Bytes(), module) {
		t.Fatalf("WASM response: %d %s %x", reply.Code, reply.Header().Get("Content-Type"), reply.Body.Bytes())
	}
}

func TestWASMLegacyMIMECacheRevalidation(t *testing.T) {
	s := fixtureApp(t)
	file := filepath.Join(s.Settings.Snapshot("").String("internal.wwwdirfull"), "web/encoder.wasm")
	if err := os.WriteFile(file, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}, 0600); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/web/encoder.wasm", nil)
	request.Header.Set("If-Modified-Since", time.Now().Add(time.Hour).UTC().Format(http.TimeFormat))
	reply := httptest.NewRecorder()
	s.Management(reply, request)
	if reply.Code != http.StatusOK || reply.Header().Get("Content-Type") != "application/wasm" {
		t.Fatalf("legacy cache retained wrong MIME: %d %s", reply.Code, reply.Header().Get("Content-Type"))
	}
	etag := reply.Header().Get("ETag")
	if etag == "" || reply.Header().Get("Last-Modified") != "" {
		t.Fatal("expected ETag-only WASM validation")
	}

	request = httptest.NewRequest(http.MethodGet, "/web/encoder.wasm", nil)
	request.Header.Set("If-None-Match", etag)
	reply = httptest.NewRecorder()
	s.Management(reply, request)
	if reply.Code != http.StatusNotModified {
		t.Fatalf("ETag cache validation: %d", reply.Code)
	}
}
