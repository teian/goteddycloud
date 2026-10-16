// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"goteddycloud/internal/content"
)

// contentAPI keeps management downloads in the selected overlay; unlike device
// requests they do not derive identity from the browser's TLS certificate.
func (s *Server) contentAPI(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	if r.Method == "POST" && strings.HasPrefix(p, "/api/fileUpload") {
		s.fileUpload(w, r)
		return true
	}
	if r.Method != "GET" && r.Method != "POST" {
		return false
	}
	get := r.Method == "GET" && strings.HasPrefix(p, "/content/json/get/")
	set := r.Method == "POST" && strings.HasPrefix(p, "/content/json/set/")
	rawJSON := r.Method == "GET" && strings.HasPrefix(p, "/content/json/")
	download := r.Method == "GET" && strings.HasPrefix(p, "/content/download/")
	raw := r.Method == "GET" && strings.HasPrefix(p, "/content/")
	if !get && !set && !rawJSON && !download && !raw {
		return false
	}
	q := r.URL.Query()
	id := q.Get("overlay")
	snap := s.Settings.Snapshot(id)
	if get || set {
		uid := strings.TrimPrefix(strings.TrimPrefix(p, "/content/json/get/"), "/content/json/set/")
		name, err := content.UIDPath(uid)
		if err != nil {
			referenceNotFound(w)
			return true
		}
		if set {
			data, err := body(r, 4095)
			if err != nil {
				audioReply(w, 413, "Request too large")
				return true
			}
			form, err := url.ParseQuery(string(data))
			if err != nil {
				audioReply(w, 400, "Invalid form")
				return true
			}
			err = s.Content.UpdateContext(r.Context(), snap, uid, func(m content.Metadata) error {
				for _, key := range []string{"source", "tonie_model"} {
					if form.Has(key) {
						m.Set(key, form.Get(key))
					}
				}
				for _, key := range []string{"live", "nocloud", "hide", "claimed"} {
					if form.Has(key) {
						m.Set(key, form.Get(key) == "true")
					}
				}
				return nil
			})
			if err != nil {
				response(w, 500, "", nil)
			} else {
				s.tagUpdated(id, uid)
				response(w, 200, "application/octet-stream", []byte("OK"))
			}
			return true
		}
		root, _, err := s.Content.ManagedRoot(snap, "")
		if err != nil {
			response(w, 500, "", nil)
			return true
		}
		f, err := root.Open(name + ".json")
		if err != nil {
			referenceNotFound(w)
			return true
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			referenceNotFound(w)
			return true
		}
		serveRangeFile(w, r, f, 0, info.Size(), "application/octet-stream")
		return true
	}
	if download {
		name := strings.TrimSuffix(strings.TrimPrefix(p, "/content/download/"), ".json")
		parts := strings.Split(name, "/")
		if len(parts) != 2 || len(parts[0]) != 8 || len(parts[1]) != 8 {
			referenceNotFound(w)
			return true
		}
		uid := strings.ToLower(parts[0] + parts[1])
		if _, err := content.UIDPath(uid); err != nil {
			referenceNotFound(w)
			return true
		}
		metadata, err := s.Content.Metadata(snap, uid)
		if err != nil {
			response(w, 500, "", nil)
			return true
		}
		cloned := r.Clone(r.Context())
		copiedURL := *r.URL
		cloned.URL = &copiedURL
		cloned.Header.Del("Authorization")
		if strings.HasPrefix(uid, "0000000") || metadata.Bool("nocloud") {
			cloned.URL.Path = "/v1/content/" + uid
		} else {
			cloned.URL.Path = "/v2/content/" + uid
			auth, err := hex.DecodeString(metadata.String("cloud_auth"))
			if err == nil && len(auth) == 32 {
				cloned.Header.Set("Authorization", "BD "+strings.ToUpper(hex.EncodeToString(auth)))
			}
		}
		s.serveStoredContent(w, cloned, id, snap, uid, false)
		return true
	}
	root, prefix, err := s.Content.ManagedRoot(snap, q.Get("special"))
	if err != nil {
		response(w, 500, "", nil)
		return true
	}
	name := strings.TrimPrefix(p, "/content/")
	kind := "audio/ogg"
	if rawJSON {
		name = strings.TrimPrefix(p, "/content/json/")
		kind = referenceMIME[strings.ToLower(filepath.Ext(p))]
		if kind == "" {
			kind = "application/octet-stream"
		}
	}
	name, err = managedPath(prefix, name)
	if err != nil {
		response(w, 400, "", nil)
		return true
	}
	f, err := root.Open(name)
	if err != nil {
		referenceNotFound(w)
		return true
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		referenceNotFound(w)
		return true
	}
	if !rawJSON && q.Has("tracks") {
		s.exportTracks(w, r, f)
		return true
	}
	offset := int64(0)
	if !rawJSON && q.Get("ogg") == "true" {
		offset = 4096
	}
	if info.Size() == offset {
		response(w, 200, kind, nil)
	} else {
		serveRangeFile(w, r, f, offset, info.Size()-offset, kind)
	}
	return true
}

func (s *Server) fileUpload(w http.ResponseWriter, r *http.Request) {
	select {
	case audioRequests <- struct{}{}:
		defer func() { <-audioRequests }()
	default:
		audioReply(w, 503, "Upload queue full")
		return
	}
	q := r.URL.Query()
	root, prefix, err := s.Content.ManagedRoot(s.Settings.Snapshot(q.Get("overlay")), q.Get("special"))
	if err != nil {
		audioReply(w, 500, "Invalid root")
		return
	}
	folder, err := managedPath(prefix, q.Get("path"))
	if err != nil {
		audioReply(w, 400, "Invalid path")
		return
	}
	info, err := root.Stat(folder)
	if err != nil || !info.IsDir() {
		audioReply(w, 500, fmt.Sprintf("invalid path: '%s'", q.Get("path")))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, audioUploadLimit)
	reader, err := r.MultipartReader()
	if err != nil {
		audioReply(w, 400, "Invalid multipart body")
		return
	}
	count := 0
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			audioReply(w, 400, "Invalid multipart body")
			return
		}
		_, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		name := params["filename"]
		if err != nil || name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\:\x00") {
			part.Close()
			audioReply(w, 400, "Invalid filename")
			return
		}
		count++
		if count > 99 {
			part.Close()
			audioReply(w, 413, "Too many files")
			return
		}
		target := filepath.Join(folder, name)
		unlock, err := root.LockContext(r.Context(), target)
		if err != nil {
			part.Close()
			return
		}
		err = root.WriteReaderAtomic(r.Context(), target, part, 0600, audioUploadLimit)
		unlock()
		part.Close()
		if err != nil {
			audioReply(w, 500, "Upload failed")
			return
		}
	}
	audioReply(w, 200, "OK")
}

// Management handlers returning ERROR_NOT_FOUND use the C server's error page.
func referenceNotFound(w http.ResponseWriter) {
	response(w, 404, "text/html", []byte("<!doctype html>\r\n<html>\r\n<head><title>Error 404</title></head>\r\n<body>\r\n<h2>Error 404</h2>\r\n<p>The requested page could not be found</p>\r\n</body>\r\n</html>\r\n"))
}
