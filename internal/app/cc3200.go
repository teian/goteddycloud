// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"goteddycloud/internal/cc3200"
	"goteddycloud/internal/certificates"
	"goteddycloud/internal/pluginassets"
	"goteddycloud/internal/storage"
)

//go:embed cc3200.html
var cc3200Page []byte

//go:embed cc3200-pinout.png
var cc3200Pinout []byte

//go:embed cc3200-ui.mjs
var cc3200UI []byte

//go:generate ../../scripts/build-cc3200-wasm.sh

//go:embed cc3200-tool.wasm
var cc3200WASM []byte

//go:embed cc3200-wasm-exec.js
var cc3200WASMExec []byte

//go:embed cc3200-go.mjs
var cc3200GoBridge []byte

//go:embed cc3200-protocol.mjs
var cc3200Protocol []byte

func (s *Server) cc3200API(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path == "/plugins/goteddycloud-cc3200/preview.png" {
		if r.Method != http.MethodGet {
			response(w, http.StatusMethodNotAllowed, "", nil)
			return true
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		response(w, http.StatusOK, "image/png", pluginassets.CC3200)
		return true
	}
	if r.URL.Path == "/plugins/goteddycloud-cc3200/plugin.json" {
		if r.Method != "GET" {
			response(w, 405, "", nil)
			return true
		}
		cc3200JSON(w, map[string]any{"pluginName": "CC3200 flashing", "author": "goteddycloud", "version": "1", "description": "Back up and flash a CC3200 with an ESP32-C3 UART gateway", "standalone": false, "teddyCloudSection": "tonieboxes", "icon": "ToolOutlined"})
		return true
	}
	if data, kind := cc3200Asset(r.URL.Path); data != nil {
		if r.Method != "GET" {
			response(w, 405, "", nil)
			return true
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self' 'wasm-unsafe-eval'; style-src 'unsafe-inline'; img-src 'self'; connect-src 'self'; frame-ancestors 'self'; base-uri 'none'; form-action 'self'")
		w.Header().Set("Permissions-Policy", "serial=(self)")
		response(w, 200, kind, data)
		return true
	}
	if !strings.HasPrefix(r.URL.Path, "/api/cc3200/") {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == "/api/cc3200/altPort" {
		if r.Method != "GET" {
			response(w, 405, "", nil)
			return true
		}
		port, err := strconv.ParseUint(r.URL.Query().Get("port"), 10, 16)
		if err != nil || port == 0 {
			audioReply(w, 400, "Port must be 1..65535")
			return true
		}
		data, err := cc3200.PortPatch(uint32(port))
		if err != nil {
			audioReply(w, 400, err.Error())
			return true
		}
		w.Header().Set("Content-Disposition", `attachment; filename="altPort.json"`)
		response(w, 200, "application/json; charset=utf-8", data)
		return true
	}
	switch r.URL.Path {
	case "/api/cc3200/inspect", "/api/cc3200/backup", "/api/cc3200/download", "/api/cc3200/import", "/api/cc3200/plan":
	default:
		referenceNotFound(w)
		return true
	}
	method := "POST"
	if r.URL.Path == "/api/cc3200/download" {
		method = "GET"
	}
	if r.Method != method {
		response(w, 405, "", nil)
		return true
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		scheme := requestScheme(r)
		if err != nil || u.Scheme != scheme || u.Host != r.Host || u.Path != "" {
			audioReply(w, 403, "Same-origin access required")
			return true
		}
	}
	select {
	case firmwareRequests <- struct{}{}:
		defer func() { <-firmwareRequests }()
	default:
		audioReply(w, 503, "Firmware queue full")
		return true
	}
	if r.URL.Path == "/api/cc3200/inspect" {
		data, err := body(r, cc3200.MaxFlash)
		if err != nil {
			audioReply(w, 400, "Invalid flash size")
			return true
		}
		m, err := cc3200.Inspect(data)
		if err != nil {
			audioReply(w, 400, err.Error())
			return true
		}
		cc3200JSON(w, m)
		return true
	}
	root, err := s.cc3200Root()
	if err != nil {
		audioReply(w, 500, "CC3200 backup storage unavailable")
		return true
	}
	defer root.Close()
	root = root.WithContext(r.Context())
	if r.URL.Path == "/api/cc3200/backup" {
		parts, err := cc3200Parts(w, r, map[string]int64{"flash": cc3200.MaxFlash, "files": cc3200.MaxArchive})
		if err != nil || len(parts["flash"]) == 0 {
			audioReply(w, 400, "Expected flash and optional named-files ZIP")
			return true
		}
		archive, m, err := cc3200.BuildBackup(parts["flash"], parts["files"])
		if err != nil {
			audioReply(w, 400, err.Error())
			return true
		}
		if err = root.WriteReaderAtomic(r.Context(), m.ID+".zip", bytes.NewReader(archive), 0600, int64(len(archive))); err != nil {
			audioReply(w, 500, "Backup publication failed")
			return true
		}
		cc3200JSON(w, m)
		return true
	}
	id := r.URL.Query().Get("backup")
	if len(id) != 64 || !hexPrefix(id, 64) {
		audioReply(w, 400, "Invalid backup ID")
		return true
	}
	f, err := root.Open(id + ".zip")
	if err != nil {
		referenceNotFound(w)
		return true
	}
	archive, err := io.ReadAll(io.LimitReader(f, cc3200.MaxBackup+1))
	f.Close()
	if err != nil || len(archive) > cc3200.MaxBackup || cc3200.Digest(archive) != id {
		audioReply(w, 500, "Backup integrity check failed")
		return true
	}
	if r.URL.Path == "/api/cc3200/download" {
		w.Header().Set("Content-Disposition", `attachment; filename="cc3200-`+id+`.zip"`)
		response(w, 200, "application/zip", archive)
		return true
	}
	files, err := cc3200.BackupFiles(archive)
	if err != nil {
		audioReply(w, 400, "Invalid backup files")
		return true
	}
	ca, client, key := files["/cert/ca.der"], files["/cert/client.der"], files["/cert/private.der"]
	if err = certificates.ValidateIdentity(ca, client, key); err != nil {
		audioReply(w, 400, "Backup certificates are missing or invalid")
		return true
	}
	if r.URL.Path == "/api/cc3200/import" {
		mac := strings.ToLower(r.URL.Query().Get("mac"))
		if len(mac) != 12 || !hexPrefix(mac, 12) {
			audioReply(w, 400, "Expected twelve hexadecimal MAC digits")
			return true
		}
		s.installExtractedCertificates(w, r, ca, client, key, mac)
		return true
	}
	manifest, err := cc3200.BackupManifest(archive)
	if err != nil || len(manifest.Missing) != 0 {
		audioReply(w, 400, "Installation requires a complete verified backup")
		return true
	}
	parts, err := cc3200Parts(w, r, map[string]int64{"preloader": 0x3c000})
	if err != nil {
		audioReply(w, 400, "Invalid preloader upload")
		return true
	}
	snap := s.Settings.Snapshot("")
	caData := []byte(snap.String("core.server_cert.data.ca"))
	if len(caData) == 0 {
		name := snap.String("core.server_cert.file.ca")
		if !filepath.IsAbs(name) {
			name = filepath.Join(snap.Base, name)
		}
		f, openErr := s.Settings.Storage().Open(name)
		if openErr != nil {
			audioReply(w, 500, "Server CA unavailable")
			return true
		}
		caData, err = io.ReadAll(io.LimitReader(f, certificates.MaxMaterial+1))
		f.Close()
	}
	replacement, parseErr := certificates.Certificate(caData)
	if err != nil || parseErr != nil || len(caData) > certificates.MaxMaterial || !replacement.IsCA {
		audioReply(w, 500, "Server CA invalid")
		return true
	}
	writes, err := cc3200.Plan(files, parts["preloader"], replacement.Raw, r.URL.Query().Get("originalSHA256"))
	if err != nil {
		audioReply(w, 400, err.Error())
		return true
	}
	cc3200JSON(w, struct {
		Writes []cc3200.Write `json:"writes"`
	}{writes})
	return true
}
func (s *Server) cc3200Root() (*storage.Root, error) {
	name := filepath.Join(s.Settings.Snapshot("").String("internal.firmwaredirfull"), "cc3200", "backups")
	if err := s.Settings.Storage().MkdirAll(name, 0700); err != nil {
		return nil, err
	}
	root, err := s.Settings.Storage().OpenRoot(name)
	if err != nil {
		return nil, err
	}
	return root, nil
}
func cc3200JSON(w http.ResponseWriter, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		audioReply(w, 500, "Response encoding failed")
		return
	}
	response(w, 200, "application/json; charset=utf-8", data)
}
func cc3200Parts(w http.ResponseWriter, r *http.Request, limits map[string]int64) (map[string][]byte, error) {
	var maximum int64 = 1 << 20
	for _, limit := range limits {
		maximum += limit
	}
	r.Body = http.MaxBytesReader(w, r.Body, maximum)
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, err
	}
	parts := map[string][]byte{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		limit, ok := limits[part.FormName()]
		if !ok || parts[part.FormName()] != nil {
			return nil, errors.New("unexpected multipart field")
		}
		data, err := io.ReadAll(io.LimitReader(part, limit+1))
		if err != nil || int64(len(data)) > limit {
			return nil, errors.New("multipart field too large")
		}
		parts[part.FormName()] = data
	}
	return parts, nil
}

func cc3200Asset(name string) ([]byte, string) {
	switch name {
	case "/tools/cc3200", "/plugins/goteddycloud-cc3200/index.html", "/plugins/goteddycloud-cc3200/":
		return cc3200Page, "text/html; charset=utf-8"
	case "/tools/cc3200-pinout.png":
		return cc3200Pinout, "image/png"
	case "/tools/cc3200-tool.wasm":
		return cc3200WASM, "application/wasm"
	case "/tools/cc3200-wasm-exec.js":
		return cc3200WASMExec, "text/javascript; charset=utf-8"
	case "/tools/cc3200-go.mjs":
		return cc3200GoBridge, "text/javascript; charset=utf-8"
	case "/tools/cc3200-ui.mjs":
		return cc3200UI, "text/javascript; charset=utf-8"
	case "/tools/cc3200-protocol.mjs":
		return cc3200Protocol, "text/javascript; charset=utf-8"
	default:
		return nil, ""
	}
}
