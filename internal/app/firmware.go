// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"goteddycloud/internal/certificates"
	"goteddycloud/internal/firmware"
	"goteddycloud/internal/storage"
)

var firmwareRequests = make(chan struct{}, 2)

func (s *Server) firmwareAPI(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	certUpload := r.Method == "POST" && strings.HasPrefix(p, "/api/uploadCert")
	der := r.Method == "GET" && (strings.HasPrefix(p, "/api/getFile/ca.der") || strings.HasPrefix(p, "/api/getFile/c2.der"))
	imageUpload := r.Method == "POST" && strings.HasPrefix(p, "/api/esp32/uploadFirmware")
	extract := r.Method == "POST" && strings.HasPrefix(p, "/api/esp32/extractCerts")
	patch := r.Method == "GET" && strings.HasPrefix(p, "/api/esp32/patchFirmware")
	if !certUpload && !der && !imageUpload && !extract && !patch {
		return false
	}
	select {
	case firmwareRequests <- struct{}{}:
		defer func() { <-firmwareRequests }()
	default:
		audioReply(w, 503, "Firmware queue full")
		return true
	}
	snap := s.Settings.Snapshot("")
	if der {
		// Both C aliases select the TB1 CA DER, including the c2.der spelling.
		name := snap.String("core.server_cert.file.ca_der")
		if !filepath.IsAbs(name) {
			name = filepath.Join(snap.Base, name)
		}
		parent, err := s.Settings.Storage().OpenRoot(filepath.Dir(name))
		if err != nil {
			referenceNotFound(w)
			return true
		}
		defer parent.Close()
		file, err := parent.Open(filepath.Base(name))
		if err != nil {
			referenceNotFound(w)
			return true
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, certificates.MaxMaterial+1))
		if len(data) > certificates.MaxMaterial {
			err = fmt.Errorf("CA DER size limit")
		}
		if err != nil {
			referenceNotFound(w)
		} else {
			response(w, 200, "application/octet-stream", data)
		}
		return true
	}
	if certUpload {
		s.uploadCertificates(w, r)
		return true
	}
	root, err := s.Settings.Storage().OpenRoot(snap.String("internal.firmwaredirfull"))
	if err != nil {
		audioReply(w, 500, "Invalid firmware root")
		return true
	}
	defer root.Close()
	if imageUpload {
		parts, err := boundedMultipart(w, r, firmware.MaxImage, 1)
		if err != nil || len(parts) != 1 {
			audioReply(w, 400, "Invalid firmware upload")
			return true
		}
		part := parts[0]
		image, err := firmware.Parse(part.data)
		if err == nil {
			_, err = image.Assets()
		}
		if err != nil {
			audioReply(w, 400, "Invalid ESP32 image")
			return true
		}
		// A directory-wide lock makes suffix selection and atomic publication one
		// operation across concurrently accepted uploads.
		code, message := func() (int, string) {
			release, err := root.LockContext(r.Context(), "firmware-uploads")
			if err != nil {
				return 503, "Firmware upload cancelled"
			}
			defer release()
			name := ""
			for suffix := 0; suffix < 100; suffix++ {
				candidate := part.name + ".bin"
				if suffix > 0 {
					candidate = fmt.Sprintf("%s_%d.bin", part.name, suffix)
				}
				if _, err := root.Lstat(candidate); os.IsNotExist(err) {
					name = candidate
					break
				}
			}
			if name == "" {
				return 409, "Firmware filename slots exhausted"
			}
			if err := root.WriteReaderAtomic(r.Context(), name, bytes.NewReader(part.data), 0600, int64(len(part.data))); err != nil {
				return 500, "Firmware upload failed"
			}
			return 200, name
		}()
		audioReply(w, code, message)
		return true
	}
	name := r.URL.Query().Get("filename")
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\:\x00") {
		referenceNotFound(w)
		return true
	}
	_, tail, found := strings.Cut(name, "_")
	if !found || len(tail) < 12 || !hexPrefix(tail, 12) {
		referenceNotFound(w)
		return true
	}
	mac := strings.ToLower(tail[:12])
	file, err := root.Open(name)
	if err != nil {
		referenceNotFound(w)
		return true
	}
	data, err := io.ReadAll(io.LimitReader(file, firmware.MaxImage+1))
	file.Close()
	if err != nil || len(data) > firmware.MaxImage {
		audioReply(w, 400, "Invalid ESP32 image")
		return true
	}
	image, err := firmware.Parse(data)
	if err != nil {
		audioReply(w, 400, "Invalid ESP32 image")
		return true
	}
	fat, err := image.Assets()
	if err != nil {
		audioReply(w, 400, "Invalid ESP32 asset filesystem")
		return true
	}
	if extract {
		s.extractCertificates(w, r, fat, mac)
		return true
	}
	caPath := snap.String("core.server_cert.file.ca")
	if !filepath.IsAbs(caPath) {
		caPath = filepath.Join(snap.Base, caPath)
	}
	caData := []byte(snap.String("core.server_cert.data.ca"))
	if len(caData) == 0 {
		var file storage.File
		file, err = s.Settings.Storage().Open(caPath)
		if err == nil {
			caData, err = io.ReadAll(io.LimitReader(file, certificates.MaxMaterial+1))
			file.Close()
		}
	}
	if err != nil {
		audioReply(w, 500, "Server CA unavailable")
		return true
	}
	ca, err := certificates.Certificate(caData)
	if err != nil {
		audioReply(w, 500, "Server CA invalid")
		return true
	}
	if err := fat.Put("CERT", "CA.DER", ca.Raw); err != nil {
		audioReply(w, 400, "CA injection failed")
		return true
	}
	fat.Commit()
	q := r.URL.Query()
	if raw := q.Get("port"); raw != "" && raw != "0" {
		port, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			audioReply(w, 400, "Invalid port")
			return true
		}
		if _, err := image.PatchPort(uint32(port)); err != nil {
			audioReply(w, 400, "Port patch failed")
			return true
		}
	}
	if host := q.Get("hostname"); host != "" {
		oldRTNL, oldAPI := "rtnl.bxcl.de", "prod.de.tbs.toys"
		if old := q.Get("hostname_old"); old != "" {
			oldRTNL = old
			oldAPI = old
		}
		if _, err := image.PatchHost(host, oldRTNL, oldAPI); err != nil {
			audioReply(w, 400, "Hostname patch failed")
			return true
		}
	}
	if q.Get("wifi_ssid") != "" {
		if err := image.PatchWiFi(q.Get("wifi_ssid"), q.Get("wifi_pass")); err != nil {
			audioReply(w, 400, "WiFi patch failed")
			return true
		}
	}
	if err := image.Fixup(); err != nil {
		audioReply(w, 400, "Image checksum repair failed")
		return true
	}
	if _, err := firmware.Parse(image.Data); err != nil {
		audioReply(w, 400, "Patched image validation failed")
		return true
	}
	release, err := root.LockContext(r.Context(), "patched_"+mac+".bin")
	if err == nil {
		err = root.WriteReaderAtomic(r.Context(), "patched_"+mac+".bin", bytes.NewReader(image.Data), 0600, int64(len(image.Data)))
		release()
	}
	if err != nil {
		audioReply(w, 500, "Patched image publication failed")
		return true
	}
	response(w, 200, "binary/octet-stream", image.Data)
	return true
}

type uploadPart struct {
	name string
	data []byte
}

func boundedMultipart(w http.ResponseWriter, r *http.Request, limit int64, count int) ([]uploadPart, error) {
	r.Body = http.MaxBytesReader(w, r.Body, limit+(64<<10))
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, err
	}
	parts := []uploadPart{}
	remaining := limit
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		_, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		name := params["filename"]
		if err != nil || name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\:\x00") || len(parts) >= count {
			part.Close()
			return nil, fmt.Errorf("invalid multipart filename/count")
		}
		data, err := io.ReadAll(io.LimitReader(part, remaining+1))
		part.Close()
		if err != nil || int64(len(data)) > remaining {
			return nil, fmt.Errorf("multipart size exceeded")
		}
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		remaining -= int64(len(data))
		parts = append(parts, uploadPart{name, data})
	}
	return parts, nil
}
func (s *Server) uploadCertificates(w http.ResponseWriter, r *http.Request) {
	parts, err := boundedMultipart(w, r, 4<<20, 16)
	if err != nil || len(parts) == 0 {
		audioReply(w, 400, "Invalid certificate upload")
		return
	}
	names := map[string][]byte{}
	for _, part := range parts {
		key := strings.ToLower(part.name)
		if _, exists := names[key]; exists {
			audioReply(w, 400, "Duplicate certificate filename")
			return
		}
		names[key] = part.data
		if strings.Contains(key, "private") || strings.Contains(key, "key") {
			_, err = certificates.PrivateKey(part.data)
		} else {
			_, err = certificates.Certificate(part.data)
		}
		if err != nil {
			audioReply(w, 400, "Invalid certificate material")
			return
		}
	}
	if names["client.der"] != nil && names["private.der"] != nil {
		ca := names["ca.der"]
		if ca == nil {
			ca = names["client.der"]
		}
		if err := certificates.ValidateIdentity(ca, names["client.der"], names["private.der"]); err != nil {
			audioReply(w, 400, "Client certificate/key mismatch")
			return
		}
	}
	path := s.Settings.Snapshot(r.URL.Query().Get("overlay")).String("internal.certdirfull")
	if err := s.Settings.Storage().MkdirAll(path, 0700); err != nil {
		audioReply(w, 500, "Certificate root unavailable")
		return
	}
	root, err := s.Settings.Storage().OpenRoot(path)
	if err != nil {
		audioReply(w, 500, "Certificate root unavailable")
		return
	}
	defer root.Close()
	code, message := func() (int, string) {
		names := make([]string, 0, len(parts))
		for _, part := range parts {
			names = append(names, part.name)
		}
		release, err := root.LockContext(r.Context(), names...)
		if err != nil {
			return 503, "Certificate upload cancelled"
		}
		defer release()
		for _, part := range parts {
			if err := root.WriteReaderAtomic(r.Context(), part.name, bytes.NewReader(part.data), 0600, int64(len(part.data))); err != nil {
				return 500, "Certificate upload failed"
			}
		}
		updates := map[string]*string{}
		snap := s.Settings.Snapshot(r.URL.Query().Get("overlay"))
		for _, part := range parts {
			key := map[string]string{"ca.der": "ca", "client.der": "crt", "private.der": "key"}[strings.ToLower(part.name)]
			if key != "" {
				path := filepath.Join(snap.String("core.certdir"), part.name)
				updates["core.client_cert.file."+key] = &path
			}
		}
		if len(updates) > 0 {
			if err := s.Settings.Update(r.URL.Query().Get("overlay"), updates); err != nil {
				return 500, "Certificate selection failed"
			}
		}

		return 200, "OK"
	}()
	audioReply(w, code, message)
}
func (s *Server) extractCertificates(w http.ResponseWriter, r *http.Request, fat *firmware.FAT, mac string) {
	files, err := fat.Extract("CERT")
	if err != nil {
		audioReply(w, 400, "Certificate extraction failed")
		return
	}
	var ca, client, key []byte
	for name, data := range files {
		switch strings.ToUpper(name) {
		case "CA.DER":
			ca = data
		case "CLIENT.DER":
			client = data
		case "PRIVATE.DER":
			key = data
		}
	}
	s.installExtractedCertificates(w, r, ca, client, key, mac)
}

func (s *Server) installExtractedCertificates(w http.ResponseWriter, r *http.Request, ca, client, key []byte, mac string) {
	if err := certificates.ValidateIdentity(ca, client, key); err != nil {
		audioReply(w, 400, "Invalid extracted certificate identity")
		return
	}
	rootPath := s.Settings.Snapshot("").String("internal.certdirfull")
	if err := s.Settings.Storage().MkdirAll(rootPath, 0700); err != nil {
		audioReply(w, 500, "Certificate root unavailable")
		return
	}
	root, err := s.Settings.Storage().OpenRoot(rootPath)
	if err != nil {
		audioReply(w, 500, "Certificate root unavailable")
		return
	}
	defer root.Close()
	code, message := func() (int, string) {
		names := []string{"ca.der", "client.der", "private.der", filepath.Join(mac, "ca.der"), filepath.Join(mac, "client.der"), filepath.Join(mac, "private.der")}
		release, err := root.LockContext(r.Context(), names...)
		if err != nil {
			return 503, "Certificate extraction cancelled"
		}
		defer release()
		materials := []uploadPart{{"ca.der", ca}, {"client.der", client}, {"private.der", key}}
		overwrite := strings.HasPrefix(r.URL.Query().Get("overwrite"), "t")
		// Preflight every conflict before changing installed identities.
		for _, part := range materials {
			existing, err := root.ReadFile(filepath.Join(mac, part.name))
			if err != nil && !os.IsNotExist(err) {
				return 500, "Certificate inspection failed"
			}
			if err == nil && !bytes.Equal(existing, part.data) && !overwrite {
				return 409, fmt.Sprintf("Different %s already exists at %s\r\n", strings.ToUpper(strings.TrimSuffix(part.name, ".der")), filepath.Join(rootPath, mac, part.name))
			}
		}
		if err := root.MkdirAll(mac, 0700); err != nil {
			return 500, "Certificate root unavailable"
		}
		for _, part := range materials {
			if err := root.WriteReaderAtomic(r.Context(), filepath.Join(mac, part.name), bytes.NewReader(part.data), 0600, int64(len(part.data))); err != nil {
				return 500, "Certificate publication failed"
			}
		}
		// The reference populates the global defaults only when absent, retaining
		// different existing global credentials even when overwriting this box.
		for _, part := range materials {
			if _, err := root.Lstat(part.name); os.IsNotExist(err) {
				if err := root.WriteReaderAtomic(r.Context(), part.name, bytes.NewReader(part.data), 0600, int64(len(part.data))); err != nil {
					return 500, "Default certificate publication failed"
				}
			}
		}
		return 200, "OK"
	}()
	audioReply(w, code, message)
}
