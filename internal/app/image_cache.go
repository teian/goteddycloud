// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"goteddycloud/internal/storage"
	"goteddycloud/internal/syncutil"
	"html"
	"net/http"
	"net/url"

	"path/filepath"
	"strings"
	"sync"
)

const imageLimit = int64(16 << 20)

type imageEntry struct {
	mu           sync.Mutex      // metadata only; never held while streaming or downloading
	work         syncutil.RWGate // one fetch or flush; waiters may cancel
	source, name string
	exists       bool
	status       int
}

//go:embed cache_stats.html
var cacheStatsPage string

// imageURL registers URLs in a bounded, exact-key index. Only registered URLs
// can be downloaded; filenames never come from the /cache request path.
func (s *Server) imageURL(ctx context.Context, source string, preload bool) string {
	return s.registerImage(ctx, source, preload, true)
}

func (s *Server) registerImage(ctx context.Context, source string, preload, fetchExisting bool) string {
	if source == "" || s.cacheRoot == nil || !s.Settings.Snapshot("").Bool("tonie_json.cache_images") {
		return source
	}
	parsed, err := url.Parse(source)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || len(source) > 4096 {
		return source
	}
	extension := "jpg"
	if n := strings.LastIndex(source, "."); n >= 0 && !strings.Contains(source[n:], "/") {
		extension = strings.SplitN(source[n+1:], "?", 2)[0]
	}
	if extension == "" || len(extension) > 16 || strings.ContainsAny(extension, "/\\:#%\x00") {
		extension = "jpg"
	}
	digest := sha256.Sum256([]byte(source))
	name := fmt.Sprintf("%X.%s", digest, extension)
	s.imagesMu.Lock()
	entry := s.images[name]
	existing := entry != nil
	s.imagesMu.Unlock()
	if entry == nil {
		candidate := &imageEntry{source: source, name: name}
		if info, err := s.cacheRoot.Stat(name); err == nil && info.Mode().IsRegular() {
			candidate.exists = true
		}
		s.imagesMu.Lock()
		entry = s.images[name]
		existing = entry != nil
		if entry == nil && len(s.images) < 4096 {
			entry = candidate
			s.images[name] = entry
		}
		s.imagesMu.Unlock()
	}
	if entry == nil {
		return source
	}
	if existing && fetchExisting || preload {
		s.fetchImage(ctx, entry)
	}
	return "/cache/" + name
}
func (s *Server) fetchImage(ctx context.Context, entry *imageEntry) {
	release, err := entry.work.Lock(ctx)
	if err != nil {
		return
	}
	defer release()
	entry.mu.Lock()
	exists := entry.exists
	entry.mu.Unlock()
	if exists {
		if info, err := s.cacheRoot.Stat(entry.name); err == nil && info.Mode().IsRegular() {
			return
		}
		entry.mu.Lock()
		entry.exists = false
		entry.mu.Unlock()
	}
	select {
	case s.imageSlots <- struct{}{}:
		defer func() { <-s.imageSlots }()
	default:
		return
	}
	req, err := http.NewRequestWithContext(ctx, "GET", entry.source, nil)
	if err != nil {
		return
	}
	res, err := s.MetadataHTTP.Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()
	exists = false
	if res.StatusCode == 200 {
		exists = s.cacheRoot.WriteReaderAtomic(ctx, entry.name, res.Body, 0600, imageLimit) == nil
	}
	entry.mu.Lock()
	entry.status, entry.exists = res.StatusCode, exists
	entry.mu.Unlock()
}

// flushImages coordinates with in-flight publication, without holding metadata
// locks while waiting for downloads. A cancelled flush stops waiting immediately.
func (s *Server) flushImages(ctx context.Context, entries []*imageEntry) int {
	deleted := 0
	for _, entry := range entries {
		release, err := entry.work.Lock(ctx)
		if err != nil {
			break
		}
		entry.mu.Lock()
		if entry.exists {
			if s.cacheRoot.Remove(entry.name) == nil {
				deleted++
			}
			entry.exists = false
		}
		entry.mu.Unlock()
		release()
	}
	return deleted
}
func (s *Server) imageStats() map[string]uint64 {
	// memory_used retains the reference 64-bit C index estimate (48-byte nodes),
	// rather than exposing an unrelated Go allocator measurement under that name.
	stats := map[string]uint64{"total_entries": 1, "exists_entries": 0, "total_files": 0, "total_size": 0, "memory_used": 48}
	s.imagesMu.Lock()
	entries := make([]*imageEntry, 0, len(s.images))
	for _, entry := range s.images {
		entries = append(entries, entry)
	}
	s.imagesMu.Unlock()
	for _, entry := range entries {
		entry.mu.Lock()
		exists := entry.exists
		entry.mu.Unlock()
		stats["total_entries"]++
		stats["memory_used"] += 48 + uint64(len(entry.source)+len("/cache/"+entry.name)+len(filepath.Join(s.cacheRoot.Name(), entry.name))+4)
		if exists {
			stats["exists_entries"]++
		}
		if info, err := s.cacheRoot.Stat(entry.name); err == nil && info.Mode().IsRegular() {
			stats["total_files"]++
			stats["total_size"] += uint64(info.Size())
		}
	}
	return stats
}
func (s *Server) cacheAPI(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	if r.Method == "GET" && strings.HasPrefix(p, "/api/cacheStats") {
		jsonReply(w, "application/json; charset=utf-8", s.imageStats())
		return true
	}
	if r.Method == "POST" && strings.HasPrefix(p, "/api/cacheFlush") {
		s.imagesMu.Lock()
		entries := make([]*imageEntry, 0, len(s.images))
		for _, entry := range s.images {
			entries = append(entries, entry)
		}
		s.imagesMu.Unlock()
		deleted := s.flushImages(r.Context(), entries)
		response(w, 200, "application/json; charset=utf-8", []byte(fmt.Sprintf("{\"message\": \"Cache successfully flushed.\", \"deleted_files\": %d}", deleted)))
		return true
	}
	if r.Method != "GET" || !strings.HasPrefix(p, "/cache/") {
		return false
	}
	if p == "/cache/stats" {
		stats := s.imageStats()
		response(w, 200, "text/html; charset=utf-8", []byte(fmt.Sprintf(cacheStatsPage, stats["total_entries"], stats["exists_entries"], stats["total_files"], stats["total_size"], stats["memory_used"])))
		return true
	}
	name := strings.TrimPrefix(p, "/cache/")
	s.imagesMu.Lock()
	entry := s.images[name]
	s.imagesMu.Unlock()
	if entry == nil {
		referenceNotFound(w)
		return true
	}
	s.fetchImage(r.Context(), entry)
	if r.Context().Err() != nil {
		return true
	}
	// Open under metadata coordination with flush, then stream independently.
	entry.mu.Lock()
	exists, status, source := entry.exists, entry.status, entry.source
	var file storage.File
	var err error
	if exists {
		file, err = s.cacheRoot.Open(entry.name)
	}
	entry.mu.Unlock()
	if !exists {
		if status == 404 {
			referenceNotFound(w)
		} else {
			w.Header().Set("Location", source)
			escaped := html.EscapeString(source)
			response(w, 301, "text/html", []byte(fmt.Sprintf("<!doctype html>\r\n<html>\r\n<head><title>Moved</title></head>\r\n<body>\r\n<h2>Moved</h2>\r\n<p>This page has moved to <a href=\"%s\">%s</a>.</p></body>\r\n</html>\r\n", escaped, escaped)))
		}
		return true
	}
	if err != nil {
		referenceNotFound(w)
		return true
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		referenceNotFound(w)
		return true
	}
	kind := referenceMIME[strings.ToLower(filepath.Ext(entry.name))]
	if kind == "" {
		kind = "application/octet-stream"
	}
	if info.Size() == 0 {
		response(w, 200, kind, nil)
	} else {
		serveRangeFile(w, r, file, 0, info.Size(), kind)
	}
	return true
}

func (s *Server) registerCatalogImages(ctx context.Context) {
	snap := s.Settings.Snapshot("")
	if !snap.Bool("tonie_json.cache_images") {
		return
	}
	for _, custom := range []bool{false, true} {
		for _, entry := range s.Catalog.Entries(custom) {
			if ctx.Err() != nil {
				return
			}
			s.registerImage(ctx, entry.String("pic"), snap.Bool("tonie_json.cache_preload"), false)
		}
	}
}
