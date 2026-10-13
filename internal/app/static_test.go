// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultTonieImagesWithoutDeploymentAssets(t *testing.T) {
	s := fixtureApp(t)
	www := s.Settings.String("", "internal.wwwdirfull")
	checkImages := func(server *Server) {
		t.Helper()
		for _, path := range []string{"/img_unknown.png", "/img_custom.png", "/img_empty.png"} {
			reply := perform(server, "GET", path, nil, false)
			if reply.Code != 200 || reply.Header().Get("Content-Type") != "image/png" {
				t.Fatal(path, reply.Code, reply.Header())
			}
			if _, err := png.Decode(bytes.NewReader(reply.Body.Bytes())); err != nil {
				t.Fatal("invalid placeholder", path, err)
			}
			if reply := perform(server, "GET", path, nil, true); reply.Code != 404 {
				t.Fatal("placeholder exposed on the device listener", reply.Code)
			}
		}
	}
	checkImages(s)
	custom := []byte("deployment placeholder")
	if err := os.WriteFile(filepath.Join(www, "img_unknown.png"), custom, 0600); err != nil {
		t.Fatal(err)
	}
	if reply := perform(s, "GET", "/img_unknown.png", nil, false); reply.Code != 200 || !bytes.Equal(reply.Body.Bytes(), custom) {
		t.Fatal("custom placeholder overridden", reply)
	}
	if err := os.RemoveAll(www); err != nil {
		t.Fatal(err)
	}
	withoutWWW, err := New(s.Settings)
	if err != nil {
		t.Fatal(err)
	}
	defer withoutWWW.Close()
	checkImages(withoutWWW)
	for _, path := range []string{"/img_private.png", "/static_images/img_unknown.png", "/web/img_unknown.png"} {
		if reply := perform(withoutWWW, "GET", path, nil, false); reply.Code != 404 {
			t.Fatal("unexpected image path exposed", path, reply.Code)
		}
	}
}

func TestBundledWebUIWithCustomDeploymentOverride(t *testing.T) {
	bundle := t.TempDir()
	if err := os.Mkdir(filepath.Join(bundle, "web"), 0755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"web/index.html": "<html><head></head><body>bundled UI</body></html>",
		"web/bundle.js":  "bundled script",
		"private.txt":    "outside web namespace",
	} {
		if err := os.WriteFile(filepath.Join(bundle, name), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GOTEDDYCLOUD_BUNDLED_WWW", bundle)
	s := fixtureApp(t)
	www := s.Settings.String("", "internal.wwwdirfull")
	if err := os.Remove(filepath.Join(www, "web/index.html")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/web", "/web/", "/web/settings/users"} {
		reply := perform(s, "GET", path, nil, false)
		if reply.Code != 200 || !strings.Contains(reply.Body.String(), "bundled UI") || !strings.Contains(reply.Body.String(), "<base href=") {
			t.Fatalf("%s: %d %s", path, reply.Code, reply.Body.String())
		}
	}
	if reply := perform(s, "GET", "/web/bundle.js", nil, false); reply.Code != 200 || reply.Body.String() != "bundled script" {
		t.Fatal(reply)
	}
	for _, path := range []string{"/private.txt", "/webfoo", "/web/missing.js"} {
		if reply := perform(s, "GET", path, nil, false); reply.Code != 404 {
			t.Fatalf("%s: unexpected status %d", path, reply.Code)
		}
	}
	if reply := perform(s, "GET", "/web/../private.txt", nil, false); reply.Code != http.StatusBadRequest {
		t.Fatal("traversal accepted", reply)
	}
	if reply := perform(s, "GET", "/web", nil, true); reply.Code != 404 {
		t.Fatal("bundle served on device listener")
	}
	if err := os.WriteFile(filepath.Join(www, "web/index.html"), []byte("custom UI"), 0600); err != nil {
		t.Fatal(err)
	}
	if reply := perform(s, "GET", "/web/settings/users", nil, false); reply.Code != 200 || reply.Body.String() != "custom UI" {
		t.Fatal("custom deployment did not override bundle", reply)
	}
	if reply := perform(s, "GET", "/web/bundle.js", nil, false); reply.Code != 404 {
		t.Fatal("mixed custom and bundled builds", reply)
	}
	// A deployment without any www directory also uses the read-only bundle.
	if err := os.RemoveAll(www); err != nil {
		t.Fatal(err)
	}
	empty, err := New(s.Settings)
	if err != nil {
		t.Fatal(err)
	}
	defer empty.Close()
	if reply := perform(empty, "GET", "/web", nil, false); reply.Code != 200 || !strings.Contains(reply.Body.String(), "bundled UI") {
		t.Fatal(reply)
	}
}

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
