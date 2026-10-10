// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if !strings.EqualFold(r.Method, "GET") {
		referenceNotFound(w)
		return
	}
	path := r.URL.Path
	if strings.HasPrefix(path, "/api/plugins/get") {
		root, err := s.Settings.Storage().OpenRoot(s.Settings.Snapshot("").String("internal.pluginsdirfull"))
		if err != nil {
			response(w, 500, "", nil)
			return
		}
		defer root.Close()
		directory, err := root.Open(".")
		if err != nil {
			response(w, 500, "", nil)
			return
		}
		defer directory.Close()
		entries, err := directory.ReadDir(-1)
		if err != nil {
			response(w, 500, "", nil)
			return
		}
		names := []string{"goteddycloud-cc3200"}
		for _, entry := range entries {
			if entry.IsDir() && entry.Name() != "goteddycloud-cc3200" && entry.Name() != "goteddycloud-users" {
				names = append(names, entry.Name())
			}
		}
		data, _ := json.Marshal(names)
		response(w, 200, "application/json; charset=utf-8", data)
		return
	}
	if path == "/" || path == "index.shtm" || path == "/index.shtm" {
		w.Header().Set("Location", "/web")
		w.Header()["Content-Type"] = []string{""}
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusMovedPermanently)
		return
	}

	if s.www == nil {
		referenceNotFound(w)
		return
	}
	if strings.HasPrefix(path, "/web") && (path == "/web" || strings.HasSuffix(path, "/") || !strings.Contains(path, ".")) {
		path = "/web/index.html"
	}
	name := strings.TrimPrefix(path, "/")
	if !filepath.IsLocal(name) {
		referenceNotFound(w)
		return
	}
	file, err := s.www.Open(name)
	if err != nil {
		referenceNotFound(w)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		referenceNotFound(w)
		return
	}
	kind := referenceMIME[strings.ToLower(filepath.Ext(name))]
	if kind == "" {
		kind = "application/octet-stream"
	}
	w.Header().Set("Content-Type", kind)
	if name == "web/index.html" {
		data, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, "Web UI unavailable", http.StatusInternalServerError)
			return
		}
		// A parser-visible base prevents speculative asset requests from resolving
		// against deep routes. Relative URLs also preserve external proxy prefixes.
		base := "./"
		if r.URL.Path == "/web" {
			base = "web/"
		} else {
			base = strings.Repeat("../", max(0, strings.Count(strings.TrimPrefix(r.URL.Path, "/web"), "/")-1)) + "./"
		}
		data = bytes.Replace(data, []byte("<head>"), []byte("<head><base href=\""+base+"\">"), 1)
		http.ServeContent(w, r, name, info.ModTime(), bytes.NewReader(data))
		return
	}
	modTime := info.ModTime()
	if kind == "application/wasm" {
		// Old clients cached these bytes as application/octet-stream. A date-only
		// 304 would retain that MIME type. Use a representation-versioned ETag and
		// omit Last-Modified so the first legacy revalidation returns the new type.
		w.Header().Set("ETag", fmt.Sprintf(`W/"wasm-v1-%x-%x"`, modTime.UnixNano(), info.Size()))
		modTime = time.Time{}
	}
	http.ServeContent(w, r, name, modTime, file)
}
