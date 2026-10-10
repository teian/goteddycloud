// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"goteddycloud/internal/cc3200"
	"goteddycloud/internal/certificates"
	"goteddycloud/internal/config"
)

func cc3200Multipart(t *testing.T, s *Server, path string, parts map[string][]byte) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	form := multipart.NewWriter(&b)
	for name, data := range parts {
		f, err := form.CreateFormFile(name, name+".bin")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = f.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	form.Close()
	r := httptest.NewRequest("POST", path, &b)
	r.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	s.Management(w, r)
	return w
}
func cc3200ARM(marker string) []byte {
	b := make([]byte, 256)
	binary.LittleEndian.PutUint32(b, 0x200040d0)
	binary.LittleEndian.PutUint32(b[4:], 0x20004065)
	copy(b[64:], marker)
	return b
}
func cc3200Input(t *testing.T, s *Server) ([]byte, []byte, []byte) {
	t.Helper()
	flash := bytes.Repeat([]byte{255}, 8*cc3200.BlockSize)
	binary.LittleEndian.PutUint16(flash, 1)
	binary.LittleEndian.PutUint16(flash[2:], 0x534c)
	copy(flash[4:], []byte{0, 1, 5, 0x80})
	original := cc3200ARM("synthetic original")
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, name := range cc3200.ImportantFiles() {
		var data []byte
		switch name {
		case "/sys/mcuimg.bin":
			data = original
		case "/sys/mcubootinfo.bin":
			data = []byte{1, 0, 0, 0}
		default:
			root, err := s.Settings.Storage().OpenRoot(s.Settings.String("", "internal.certdirfull"))
			if err != nil {
				t.Fatal(err)
			}
			filename := map[string]string{"ca.der": "ca.der", "client.der": "teddy-cert.pem", "private.der": "teddy-key.pem"}[filepath.Base(name)]
			data, err = root.ReadFile(filepath.Join("001122334455", filename))
			root.Close()
			if err == nil {
				data, err = certificates.DER(data)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		f, err := z.Create(strings.TrimPrefix(name, "/"))
		if err != nil {
			t.Fatal(err)
		}
		f.Write(data)
	}
	z.Close()
	return flash, b.Bytes(), original
}
func TestCC3200BackupPlanAndCertificates(t *testing.T) {
	for _, kind := range []string{"filesystem", "s3"} {
		t.Run(kind, func(t *testing.T) {
			var s *Server
			if kind == "filesystem" {
				s = fixtureApp(t)
			} else {
				base, backend := remoteFixture(t)
				settings, err := config.OpenWithStorage(base, backend, false)
				if err != nil {
					t.Fatal(err)
				}
				s, err = New(settings)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { s.Close(); settings.Close() })
			}
			flash, named, original := cc3200Input(t, s)
			w := perform(s, "POST", "/api/cc3200/inspect", flash, false)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			w = cc3200Multipart(t, s, "/api/cc3200/backup", map[string][]byte{"flash": flash, "files": named})
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			var m cc3200.Manifest
			if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil || len(m.Missing) != 0 || len(m.ID) != 64 {
				t.Fatal(m, err)
			}
			download := perform(s, "GET", "/api/cc3200/download?backup="+m.ID, nil, false)
			if download.Code != 200 || cc3200.Digest(download.Body.Bytes()) != m.ID || download.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(download.Code, download.Body.String())
			}
			files, err := cc3200.BackupFiles(download.Body.Bytes())
			if err != nil || !bytes.Equal(files["/sys/mcuimg.bin"], original) {
				t.Fatal(err)
			}
			preloader := cc3200ARM("HackieboxNG preloader synthetic")
			w = cc3200Multipart(t, s, "/api/cc3200/plan?backup="+m.ID+"&originalSHA256="+cc3200.Digest(original), map[string][]byte{"preloader": preloader})
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			var plan struct {
				Writes []cc3200.Write `json:"writes"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil || len(plan.Writes) != 3 || !bytes.Equal(plan.Writes[0].Data, original) {
				t.Fatal(err, plan)
			}
			if _, err := s.Settings.Storage().Stat(filepath.Join(s.Settings.String("", "internal.certdirfull"), "112233445566")); err == nil {
				t.Fatal("plan modified certificates")
			}
			w = perform(s, "POST", "/api/cc3200/import?backup="+m.ID+"&mac=112233445566", nil, false)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			root, err := s.Settings.Storage().OpenRoot(s.Settings.String("", "internal.certdirfull"))
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			got, err := root.ReadFile("112233445566/private.der")
			if err != nil || !bytes.Equal(got, files["/cert/private.der"]) {
				t.Fatal("identity import", err)
			}
			if err := root.WriteAtomic("112233445566/ca.der", []byte("existing"), 0600); err != nil {
				t.Fatal(err)
			}
			if w = perform(s, "POST", "/api/cc3200/import?backup="+m.ID+"&mac=112233445566", nil, false); w.Code != 409 {
				t.Fatal(w.Code, w.Body.String())
			}
			got, err = root.ReadFile("112233445566/ca.der")
			if err != nil || string(got) != "existing" {
				t.Fatal("identity conflict overwritten", err)
			}
			w = cc3200Multipart(t, s, "/api/cc3200/plan?backup="+m.ID+"&originalSHA256="+strings.Repeat("0", 64), map[string][]byte{"preloader": preloader})
			if w.Code != 400 {
				t.Fatal("incorrect original accepted", w.Code)
			}
			// A fresh Server reopens the same durable backup, including S3 deployments.
			fresh, err := New(s.Settings)
			if err != nil {
				t.Fatal(err)
			}
			defer fresh.Close()
			if w = perform(fresh, "GET", "/api/cc3200/download?backup="+m.ID, nil, false); w.Code != 200 || !bytes.Equal(w.Body.Bytes(), download.Body.Bytes()) {
				t.Fatal("restart backup", w.Code)
			}
		})
	}
}
func TestCC3200SecurityAndBounds(t *testing.T) {
	s := fixtureApp(t)
	for _, path := range []string{"/tools/cc3200", "/tools/cc3200-ui.mjs", "/tools/cc3200-protocol.mjs", "/tools/cc3200-go.mjs", "/tools/cc3200-wasm-exec.js", "/tools/cc3200-tool.wasm", "/plugins/goteddycloud-cc3200/index.html"} {
		w := perform(s, "GET", path, nil, false)
		if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'self'") {
			t.Fatal(path, w.Code)
		}
	}
	wasm := perform(s, "GET", "/tools/cc3200-tool.wasm", nil, false)
	if wasm.Header().Get("Content-Type") != "application/wasm" || !bytes.HasPrefix(wasm.Body.Bytes(), []byte{0, 'a', 's', 'm'}) {
		t.Fatal("invalid embedded Go WASM module")
	}
	list := perform(s, "GET", "/api/plugins/getPlugins", nil, false)
	var plugins []string
	if err := json.Unmarshal(list.Body.Bytes(), &plugins); err != nil || !strings.Contains(list.Body.String(), "goteddycloud-cc3200") {
		t.Fatal(list.Code, err)
	}
	meta := perform(s, "GET", "/plugins/goteddycloud-cc3200/plugin.json", nil, false)
	var metadata map[string]any
	if err := json.Unmarshal(meta.Body.Bytes(), &metadata); err != nil || metadata["pluginName"] != "CC3200 flashing" || metadata["teddyCloudSection"] != "tonieboxes" {
		t.Fatal(meta.Code, err)
	}
	if w := perform(s, "GET", "/api/cc3200/download?backup=../private", nil, false); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := perform(s, "POST", "/api/cc3200/inspect", []byte("invalid"), false); w.Code != 400 {
		t.Fatal(w.Code)
	}
	flash, _, _ := cc3200Input(t, s)
	w := cc3200Multipart(t, s, "/api/cc3200/backup", map[string][]byte{"flash": flash})
	var m cc3200.Manifest
	json.Unmarshal(w.Body.Bytes(), &m)
	if w.Code != 200 || len(m.Missing) != 5 {
		t.Fatal(w.Code, m)
	}
	w = cc3200Multipart(t, s, "/api/cc3200/plan?backup="+m.ID, map[string][]byte{"preloader": cc3200ARM("HackieboxNG preloader")})
	if w.Code != 400 {
		t.Fatal("incomplete backup accepted", w.Code)
	}
	r := httptest.NewRequest("POST", "/api/cc3200/inspect", bytes.NewReader(flash))
	r.Header.Set("Origin", "https://other.invalid")
	w = httptest.NewRecorder()
	s.Management(w, r)
	if w.Code != 403 {
		t.Fatal("cross origin accepted", w.Code)
	}
	for _, request := range []struct {
		path string
		body string
	}{{"/api/auth/users/create", `{"username":"admin","password":"secret"}`}, {"/api/auth/enabled", `{"enabled":true}`}} {
		if w := perform(s, "POST", request.path, []byte(request.body), false); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/tools/cc3200", "/api/cc3200/download?backup=" + m.ID, "/api/cc3200/inspect", "/api/cc3200/altPort?port=10443"} {
		if w := perform(s, "GET", path, nil, false); w.Code != 401 {
			t.Fatal("unauthenticated access", path, w.Code)
		}
	}
	if w := perform(s, "POST", "/api/cc3200/inspect", flash, true); w.Code != 404 {
		t.Fatal("box exposed management", w.Code)
	}
}

func TestCC3200PortPatchDownload(t *testing.T) {
	s := fixtureApp(t)
	for _, port := range []string{"1", "443", "10443", "65535"} {
		w := perform(s, "GET", "/api/cc3200/altPort?port="+port, nil, false)
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil {
			t.Fatal(err)
		}
		want, err := cc3200.PortPatch(uint32(n))
		if err != nil || w.Code != 200 || !bytes.Equal(w.Body.Bytes(), want) || w.Header().Get("Content-Disposition") != `attachment; filename="altPort.json"` {
			t.Fatal(port, w.Code, w.Body.String(), err)
		}
	}
	for _, port := range []string{"", "0", "-1", "65536", "1.5", "abc"} {
		if w := perform(s, "GET", "/api/cc3200/altPort?port="+port, nil, false); w.Code != 400 {
			t.Fatal(port, w.Code)
		}
	}
	if w := perform(s, "POST", "/api/cc3200/altPort?port=10443", nil, false); w.Code != 405 {
		t.Fatal(w.Code)
	}
	if w := perform(s, "GET", "/api/cc3200/altPort?port=10443", nil, true); w.Code != 404 {
		t.Fatal("box listener exposed management patch", w.Code)
	}
}
