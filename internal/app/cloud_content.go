// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"io"
	"net/http"

	"goteddycloud/internal/config"
	"goteddycloud/internal/content"
)

func (s *Server) serveCloudContent(w http.ResponseWriter, r *http.Request, snap config.Snapshot, ruid string, metadata content.Metadata, refresh bool) {
	unlock, err := s.Content.DownloadLock(r.Context(), snap, ruid)
	if err != nil {
		response(w, 502, "", nil)
		return
	}
	defer unlock()
	// A preceding miss may have completed while this request waited.
	if !refresh {
		if file, _, err := s.Content.OpenContext(r.Context(), snap, s.Settings.Snapshot(""), ruid); err == nil {
			file.Close()
			// Serve the just-published local file without reentering cloud policy.
			s.serveStoredLocal(w, r, snap, ruid)
			return
		}
	}
	path, headers := cloudTagRequest(r, metadata, ruid)
	res, err := s.doCloud(r.Context(), snap, s.Settings.Snapshot(""), r.Method, path, r.URL.RawQuery, headers, nil)
	if err != nil {
		response(w, 502, "", nil)
		return
	}
	defer res.Body.Close()
	var cache *content.CacheFile
	// A ranged or header-stripped body is never eligible for full-file publication.
	if snap.Bool("cloud.cacheContent") && res.StatusCode == 200 && r.Header.Get("Range") == "" && r.URL.Query().Get("skip_header") == "" {
		cache, _ = s.Content.BeginCache(snap, ruid)
		if cache != nil {
			defer cache.Close()
		}
	}
	for _, key := range []string{"Content-Type", "Content-Length", "Content-Range", "ETag", "Last-Modified", "Cache-Control"} {
		if value := res.Header.Get(key); value != "" {
			w.Header().Set(key, value)
		}
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.WriteHeader(res.StatusCode)
	var destination io.Writer = w
	if cache != nil {
		destination = io.MultiWriter(w, cache)
	}
	if _, err := io.Copy(destination, res.Body); err != nil {
		return
	}
	if cache != nil && cache.Commit(r.Context(), res.ContentLength) == nil {
		// Publish before changing the pointer so a failed transfer preserves the
		// existing source mapping and usable cache. Unknown metadata stays intact.
		_ = s.Content.UpdateContext(r.Context(), snap, ruid, func(m content.Metadata) error { m.Set("source", ""); return nil })
		if snap.Bool("cloud.cacheToLibrary") {
			_ = s.Content.CacheToLibrary(r.Context(), snap, ruid)
		}
	}
}
