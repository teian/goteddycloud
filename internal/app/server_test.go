// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"

	"goteddycloud/internal/config"
	"goteddycloud/internal/fixtures"
	"goteddycloud/internal/protocol"
)

func fixtureApp(t testing.TB) *Server {
	t.Helper()
	base := filepath.Join(t.TempDir(), "fixture")
	if err := fixtures.Generate(base); err != nil {
		t.Fatal(err)
	}
	settings, err := config.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(settings)
	if err != nil {
		settings.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(); settings.Close() })
	return s
}
func perform(s *Server, method, path string, data []byte, device bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	w := httptest.NewRecorder()
	if device {
		s.Device(w, r)
	} else {
		s.Management(w, r)
	}
	return w
}
func TestFreshnessSettingsValidationAndClaim(t *testing.T) {
	s := fixtureApp(t)
	reply := perform(s, "POST", "/v1/freshness-check", nil, true)
	if reply.Code != 200 {
		t.Fatal(reply.Code)
	}
	decoded := new(protocol.TonieFreshnessCheckResponse)
	if err := proto.Unmarshal(reply.Body.Bytes(), decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.GetMaxVolSpk() != 3 || decoded.GetMaxVolHdp() != 3 || decoded.GetSlapEn() != 1 {
		t.Fatal(decoded)
	}
	for _, data := range [][]byte{{0x0a, 2, 0x09, 0}, bytes.Repeat([]byte{0}, 4096)} {
		w := perform(s, "POST", "/v1/freshness-check", data, true)
		if w.Code < 400 {
			t.Fatal("malformed request accepted")
		}
	}
	reply = perform(s, "GET", "/v1/claim/0000000100000001", nil, true)
	if reply.Code != 200 {
		t.Fatal(reply)
	}
	m, err := s.Content.Metadata(s.Settings.Snapshot(""), "0000000100000001")
	if err != nil || !m.Bool("claimed") || m.Bool("hide") || m.String("fixture_extension") != "preserve-me" {
		t.Fatal(m, err)
	}
	if w := perform(s, "GET", "/api/getBoxes", nil, true); w.Code != 404 {
		t.Fatal("management exposed on box listener")
	}
}
func TestSettingsMutationsAreAtomicAndIsolated(t *testing.T) {
	s := fixtureApp(t)
	base := s.Settings.Snapshot("").Base
	path := "/api/settings/set/toniebox.max_vol_spk?overlay=001122334455"
	w := perform(s, "POST", path, []byte("2"), false)
	if w.Code != 200 || w.Body.Len() != 0 {
		t.Fatal(w)
	}
	if got := s.Settings.Snapshot("001122334455").Uint("toniebox.max_vol_spk"); got != 2 {
		t.Fatal(got)
	}
	if got := s.Settings.Snapshot("AABBCCDDEEFF").Uint("toniebox.max_vol_spk"); got != 3 {
		t.Fatal("other box changed")
	}
	file := filepath.Join(base, "config/config.overlay.ini")
	before, _ := os.ReadFile(file)
	perform(s, "POST", path, []byte("5"), false)
	after, _ := os.ReadFile(file)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid mutation changed file")
	}
	restarted, err := config.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if restarted.Snapshot("001122334455").Uint("toniebox.max_vol_spk") != 2 {
		t.Fatal("lost persisted value")
	}
	// Unknown overlay fields must not prevent removal or damage another overlay.
	if err := os.WriteFile(file, append(after, []byte("overlay.001122334455.future.extension=preserved\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	restored, err := config.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err := restored.RemoveOverlay("001122334455"); err != nil {
		t.Fatal(err)
	}
	if len(restored.BoxIDs()) != 1 || restored.BoxIDs()[0] != "AABBCCDDEEFF" {
		t.Fatal(restored.BoxIDs())
	}
	if w := perform(s, http.MethodGet, "/api/settings/get/toniebox.max_vol_spk?overlay=001122334455", nil, false); w.Header().Get("Content-Type") != "text/plain" {
		t.Fatal(w.Header())
	}
}
