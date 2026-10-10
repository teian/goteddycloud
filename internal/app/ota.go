// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"goteddycloud/internal/config"
	"goteddycloud/internal/storage"
)

const maxOTABytes = 64 << 20

func otaDirectory(snap config.Snapshot, fileID uint64) string {
	chips := map[uint64]string{1: "cc3200", 2: "cc3235", 3: "esp32", 4: "tb2"}
	return filepath.Join("ota", chips[snap.Uint("internal.toniebox_firmware.boxIC")], strconv.FormatUint(fileID, 10))
}
func decimalPrefix(value string) uint64 {
	end := 0
	for end < len(value) && value[end] >= '0' && value[end] <= '9' {
		end++
	}
	n, _ := strconv.ParseUint(value[:end], 10, 64)
	return n
}
func newestOTA(root *storage.Root, directory string) (string, uint64) {
	newest := uint64(1)
	name := ""
	dir, err := root.Open(directory)
	if err != nil {
		return name, newest
	}
	defer dir.Close()
	files, err := dir.ReadDir(-1)
	if err != nil {
		return name, newest
	}
	for _, file := range files {
		if file.IsDir() || strings.Contains(file.Name(), ".tmp") {
			continue
		}
		timestamp := decimalPrefix(file.Name())
		if timestamp > newest {
			name, newest = file.Name(), timestamp
		}
	}
	return name, newest
}
func (s *Server) otaV1(w http.ResponseWriter, r *http.Request, id string, snap config.Snapshot) {
	raw := strings.TrimPrefix(r.URL.Path, "/v1/ota/")
	fileID := decimalPrefix(raw)
	if fileID > 255 || raw == "" {
		response(w, 400, "", nil)
		return
	}
	timestamp := decimalPrefix(r.URL.Query().Get("cv"))
	keys := map[uint64]string{2: "otaVersionPd", 3: "otaVersionEu", 4: "otaVersionServicePack", 5: "otaVersionHtml", 6: "otaVersionSfx"}
	if key := keys[fileID]; key != "" {
		_ = s.Settings.SetRuntime(id, map[string]string{"internal.toniebox_firmware." + key: strconv.FormatUint(timestamp, 10)})
	}
	root, err := s.Settings.Storage().OpenRoot(snap.String("internal.firmwaredirfull"))
	if err != nil {
		response(w, 304, "", nil)
		return
	}
	defer root.Close()
	directory := otaDirectory(snap, fileID)
	name, newest := newestOTA(root, directory)
	if enabled(snap, "enableV1Ota") {
		query := r.URL.RawQuery
		if snap.Bool("cloud.cacheOta") {
			query = "cv=" + strconv.FormatUint(newest, 10)
		}
		res, err := s.doCloud(r.Context(), snap, s.Settings.Snapshot(""), r.Method, r.URL.Path, query, cloudHeaders(r.Header), nil)
		if err == nil {
			defer res.Body.Close()
			if !snap.Bool("cloud.cacheOta") {
				for _, key := range []string{"Content-Type", "Content-Length", "Content-Disposition", "Content-Range"} {
					if value := res.Header.Get(key); value != "" {
						w.Header().Set(key, value)
					}
				}
				w.WriteHeader(res.StatusCode)
				_, _ = io.Copy(w, res.Body)
				return
			}
			if res.StatusCode == 200 {
				cacheOTA(r, root, directory, res)
			}
		} else if !snap.Bool("cloud.cacheOta") {
			response(w, 502, "", nil)
			return
		}
	}
	// The reference chooses before fetching: a newly cached version becomes
	// eligible on the next request, while the previously selected file is served.
	if name != "" && timestamp < newest && snap.Bool("cloud.localOta") {
		file, err := root.Open(filepath.Join(directory, name))
		if err == nil {
			defer file.Close()
			info, err := file.Stat()
			if err == nil && info.Mode().IsRegular() {
				kind := referenceMIME[strings.ToLower(filepath.Ext(name))]
				if kind == "" {
					kind = "application/octet-stream"
				}
				serveRangeFile(w, r, file, 0, info.Size(), kind)
				return
			}
		}
	}
	response(w, 304, "", nil)
}
func cacheOTA(r *http.Request, root *storage.Root, directory string, res *http.Response) {
	filename, ok := strings.CutPrefix(res.Header.Get("Content-Disposition"), "attachment;filename=")
	if !ok {
		return
	}
	if index := strings.IndexAny(filename, "/\\"); index >= 0 {
		filename = filename[:index]
	}
	if !filepath.IsLocal(filename) || filename == "." || strings.Contains(filename, ":") {
		return
	}
	if res.ContentLength <= 0 || res.ContentLength > maxOTABytes {
		return
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxOTABytes+1))
	if err != nil || int64(len(data)) != res.ContentLength || r.Context().Err() != nil {
		return
	}
	name := filepath.Join(directory, filename)
	unlock, err := root.LockContext(r.Context(), name)
	if err != nil {
		return
	}
	defer unlock()
	if file, err := root.Open(name); err == nil {
		file.Close()
		return
	} else if !os.IsNotExist(err) {
		return
	}
	if err := root.MkdirAll(directory, 0700); err != nil {
		return
	}
	if r.Context().Err() == nil {
		_ = root.WriteAtomic(name, data, 0600)
	}
}

func (s *Server) otaV3(w http.ResponseWriter, r *http.Request, snap config.Snapshot) {
	rest := strings.TrimPrefix(r.URL.Path, "/v3/ota/")
	fileIDText, hash, ok := strings.Cut(rest, "/")
	fileID := decimalPrefix(fileIDText)
	if !ok || fileID > 255 || len(hash) == 0 || len(hash) > 128 || !plainHex(hash) || !filepath.IsLocal(hash) || strings.ContainsAny(hash, "/\\:") {
		response(w, 400, "", nil)
		return
	}
	root, err := s.Settings.Storage().OpenRoot(snap.String("internal.firmwaredirfull"))
	if err != nil {
		response(w, 404, "", nil)
		return
	}
	defer root.Close()
	directory := otaDirectory(snap, fileID)
	if enabled(snap, "enableV3Ota") {
		res, err := s.doCloud(r.Context(), snap, s.Settings.Snapshot(""), r.Method, r.URL.Path, r.URL.RawQuery, cloudHeaders(r.Header), nil)
		if err == nil {
			defer res.Body.Close()
			if !snap.Bool("cloud.cacheOta") {
				for _, key := range []string{"Content-Type", "Content-Length", "Content-Disposition", "Content-Range"} {
					if value := res.Header.Get(key); value != "" {
						w.Header().Set(key, value)
					}
				}
				w.WriteHeader(res.StatusCode)
				_, _ = io.Copy(w, res.Body)
				return
			}
			if res.StatusCode == 200 {
				if res.Header.Get("Content-Disposition") == "" {
					res.Header.Set("Content-Disposition", "attachment;filename="+hash+".bin")
				}
				cacheOTA(r, root, directory, res)
			}
		} else if !snap.Bool("cloud.cacheOta") {
			response(w, 502, "", nil)
			return
		}
	}
	if snap.Bool("cloud.localOta") {
		file, err := root.Open(filepath.Join(directory, hash+".bin"))
		if err == nil {
			defer file.Close()
			info, err := file.Stat()
			if err == nil && info.Mode().IsRegular() {
				serveRangeFile(w, r, file, 0, info.Size(), "application/octet-stream")
				return
			}
		}
	}
	response(w, 404, "", nil)
}

func plainHex(v string) bool {
	for _, c := range v {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return v != ""
}
