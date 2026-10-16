// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

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
	res, err := s.doCloud(tagCloudContext(r, ruid, metadata, headers), snap, s.Settings.Snapshot(""), r.Method, path, r.URL.RawQuery, headers, nil)
	if err != nil {
		response(w, 502, "", nil)
		return
	}
	defer res.Body.Close()
	logger := slog.Default().With("box", snap.String("commonName"), "tag_prefix", cloudLogTagPrefix(path))
	var cache *content.CacheFile
	// A ranged or header-stripped body is never eligible for full-file publication.
	skipCache := ""
	switch {
	case !snap.Bool("cloud.cacheContent"):
		skipCache = "caching_disabled"
	case res.StatusCode != 200:
		skipCache = "upstream_status"
	case r.Header.Get("Range") != "":
		skipCache = "ranged_request"
	case r.URL.Query().Get("skip_header") != "":
		skipCache = "header_stripped_request"
	}
	logger.Info("Tonies audio cache policy", "cache_content", snap.Bool("cloud.cacheContent"), "cache_to_library", snap.Bool("cloud.cacheToLibrary"),
		"content_root", snap.String("internal.contentdirfull"), "library_root", snap.String("internal.librarydirfull"))
	if skipCache == "" {
		cache, err = s.Content.BeginCache(snap, ruid)
		if err != nil {
			logger.Warn("Tonies audio cache failed", "stage", "initialize", "reason", cacheFailureReason(err))
		} else {
			defer cache.Close()
		}
	} else {
		logger.Info("Tonies audio cache skipped", "reason", skipCache)
	}
	for _, key := range []string{"Content-Type", "Content-Length", "Content-Range", "ETag", "Last-Modified", "Cache-Control"} {
		if value := res.Header.Get(key); value != "" {
			w.Header().Set(key, value)
		}
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	// A Tonies 401 is a cloud rejection, not an expired management session.
	if res.StatusCode >= 400 {
		w.Header().Set("X-TeddyCloud-Error-Origin", "tonies-cloud")
	}
	w.WriteHeader(res.StatusCode)
	var destination io.Writer = w
	if cache != nil {
		destination = io.MultiWriter(w, cache)
	}
	transferred, err := io.Copy(destination, res.Body)
	if err != nil {
		logger.Warn("Tonies audio download interrupted", "status", res.StatusCode, "bytes", transferred, "reason", cacheFailureReason(err))
		return
	}
	logger.Info("Tonies audio download completed", "status", res.StatusCode, "bytes", transferred)
	if cache == nil {
		return
	}
	// The complete response may already have reached a client that closes its
	// connection. Finish publishing the validated file independently of that
	// disconnect, but bound finalization and never detach an incomplete transfer.
	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer cancel()
	if err := cache.Commit(publishCtx, res.ContentLength); err != nil {
		logger.Warn("Tonies audio cache failed", "stage", "publish", "reason", cacheFailureReason(err), "bytes", transferred)
		return
	}
	logger.Info("Tonies audio cache published", "bytes", transferred)
	// Publish before changing the pointer so a failed transfer preserves the
	// existing source mapping and usable cache. Unknown metadata stays intact.
	if err := s.Content.UpdateContext(publishCtx, snap, ruid, func(m content.Metadata) error { m.Set("source", ""); return nil }); err != nil {
		logger.Warn("Tonies audio cache failed", "stage", "metadata_update", "reason", cacheFailureReason(err))
		return
	}
	if snap.Bool("cloud.cacheToLibrary") {
		if err := s.Content.CacheToLibrary(publishCtx, snap, ruid); err != nil {
			logger.Warn("Tonies audio library cache failed", "reason", cacheFailureReason(err), "content_cache_published", true)
			return
		}
		logger.Info("Tonies audio library cache published", "bytes", transferred)
	}
}
