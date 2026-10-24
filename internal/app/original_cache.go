// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"goteddycloud/internal/config"
	"goteddycloud/internal/content"
)

type backgroundCloudCacheKey struct{}

type originalCacheState struct {
	mu      sync.Mutex
	closed  bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	slots   chan struct{}
	pending map[string]bool
	retry   map[string]time.Time
}

func originalCacheEligible(snap config.Snapshot, ruid string, m content.Metadata) bool {
	if _, err := content.UIDPath(ruid); err != nil {
		return false
	}
	return snap.Bool("toniebox.api_access") && snap.Bool("cloud.enabled") && snap.Bool("cloud.enableV2Content") && snap.Bool("cloud.cacheContent") &&
		strings.HasSuffix(strings.ToLower(ruid), "0304e0") && !m.Bool("nocloud") && !m.Bool("live") && !m.Bool("cloud_override") &&
		m.String("source") == "" && hasCloudAuth(m)
}

// Authenticated claims and RTNL presentations can arrive even when the box plays
// its own cached audio. Backfill missing original audio independently of claims.
func (s *Server) scheduleOriginalCache(id, ruid string, m content.Metadata, userAgent string) {
	ruid = strings.ToLower(ruid)
	snap := s.Settings.Snapshot(id)
	if !originalCacheEligible(snap, ruid, m) {
		return
	}
	key := snap.String("internal.contentdirfull") + ":" + ruid
	q := &s.originalCache
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	if q.ctx == nil {
		q.ctx, q.cancel = context.WithCancel(context.Background())
		q.slots = make(chan struct{}, 2)
		q.pending = make(map[string]bool)
		q.retry = make(map[string]time.Time)
	}
	now := time.Now()
	for k, deadline := range q.retry {
		if !now.Before(deadline) {
			delete(q.retry, k)
		}
	}
	if q.pending[key] {
		slog.Info("Tonies original cache skipped", "box", id, "tag_prefix", ruid[:8], "reason", "already_pending")
		return
	}
	if now.Before(q.retry[key]) {
		slog.Info("Tonies original cache deferred", "box", id, "tag_prefix", ruid[:8], "reason", "retry_backoff")
		return
	}
	if len(q.pending) >= 16 {
		slog.Warn("Tonies original cache queue full", "box", id, "tag_prefix", ruid[:8])
		return
	}
	if len(userAgent) > 512 {
		userAgent = ""
	}
	q.pending[key] = true
	q.wg.Add(1)
	slog.Info("Tonies original cache queued", "box", id, "tag_prefix", ruid[:8])
	go func() {
		success := false
		defer func() {
			q.mu.Lock()
			delete(q.pending, key)
			if !success && q.ctx.Err() == nil {
				if len(q.retry) >= 128 {
					for k := range q.retry {
						delete(q.retry, k)
						break
					}
				}
				q.retry[key] = time.Now().Add(time.Minute)
			}
			q.mu.Unlock()
			q.wg.Done()
		}()
		select {
		case q.slots <- struct{}{}:
			defer func() { <-q.slots }()
		case <-q.ctx.Done():
			return
		}
		ctx, cancel := context.WithTimeout(q.ctx, 2*time.Minute)
		defer cancel()
		if ctx.Err() != nil {
			return
		}
		success = s.cacheOriginal(ctx, id, ruid, userAgent)
	}()
}

func (s *Server) stopOriginalCache() {
	q := &s.originalCache
	q.mu.Lock()
	q.closed = true
	if q.cancel != nil {
		q.cancel()
	}
	q.mu.Unlock()
	q.wg.Wait()
}

func (s *Server) cacheOriginal(ctx context.Context, id, ruid, userAgent string) bool {
	if id != "" && !s.Settings.HasBox(id) {
		return true
	}
	if s.parent != nil {
		owner, ok := s.parent.boxOwner(id)
		if !ok || owner.ID != s.areaID {
			return true
		}
	}
	snap := s.Settings.Snapshot(id)
	unlock, err := s.Content.DownloadLock(ctx, snap, ruid)
	if err != nil {
		return false
	}
	defer unlock()
	m, err := s.Content.MetadataContext(ctx, snap, ruid)
	if err != nil {
		return false
	}
	// Recheck after waiting: settings, credentials or local assignments may change.
	if !originalCacheEligible(snap, ruid, m) {
		return true
	}
	logger := slog.Default().With("box", id, "tag_prefix", ruid[:8])
	defer s.tagUpdated(id, ruid)
	if file, _, err := s.Content.OpenContext(ctx, snap, s.Settings.Snapshot(""), ruid); err == nil {
		file.Close()
		// An earlier transfer or a disabled library setting may have left a
		// valid UID cache. Finish publication without downloading it again.
		if snap.Bool("cloud.cacheToLibrary") {
			if err := s.Content.CacheToLibrary(ctx, snap, ruid); err != nil {
				logger.Warn("Tonies original library cache failed", "reason", cacheFailureReason(err))
				return false
			}
			logger.Info("Tonies original library cache published")
		}
		return true
	}
	logger.Info("Tonies original cache started")
	ctx = context.WithValue(ctx, backgroundCloudCacheKey{}, true)
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, "/v2/content/"+ruid, nil)
	if err != nil {
		return false
	}
	r.Header.Set("Authorization", "BD "+strings.ToUpper(m.String("cloud_auth")))
	if userAgent != "" {
		r.Header.Set("User-Agent", userAgent)
	}
	w := &cacheDiscardWriter{header: make(http.Header)}
	s.serveCloudContentLocked(w, r, snap, ruid, m, false)
	if file, _, err := s.Content.OpenContext(ctx, snap, s.Settings.Snapshot(""), ruid); err == nil {
		file.Close()
		if snap.Bool("cloud.cacheToLibrary") {
			published, err := s.Content.MetadataContext(ctx, snap, ruid)
			if err != nil || published.String("source") == "" {
				logger.Warn("Tonies original cache incomplete", "status", w.status, "bytes", w.bytes, "reason", "library_not_published")
				return false
			}
		}
		logger.Info("Tonies original cache completed", "status", w.status, "bytes", w.bytes)
		return true
	}
	logger.Warn("Tonies original cache failed", "status", w.status, "bytes", w.bytes, "reason", "cache_not_published")
	return false
}

// Discard the response body while the common cloud path validates and publishes
// its cache. A large Tonie must never be buffered in memory by the worker.
type cacheDiscardWriter struct {
	header http.Header
	status int
	bytes  int64
}

func (w *cacheDiscardWriter) Header() http.Header { return w.header }
func (w *cacheDiscardWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *cacheDiscardWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	w.bytes += int64(len(data))
	return len(data), nil
}
