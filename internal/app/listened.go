// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"goteddycloud/internal/config"
	"goteddycloud/internal/storage"
	"goteddycloud/internal/taf"
)

func setListened(ctx context.Context, root *storage.Root, name string, value bool) error {
	unlock, err := root.LockContext(ctx, name)
	if err != nil {
		return err
	}
	defer unlock()
	m := fileMetadata(root, name)
	if m.Bool("listened") == value {
		return nil
	}
	m.Set("listened", value)
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return root.WriteReaderAtomic(ctx, name+".json", bytes.NewReader(data), 0600, int64(len(data)))
}
func (s *Server) listenedAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != "POST" || !strings.HasPrefix(r.URL.Path, "/api/fileSetListened") {
		return false
	}
	q := r.URL.Query()
	root, prefix, err := s.Content.ManagedRoot(s.Settings.Snapshot(q.Get("overlay")), q.Get("special"))
	if err != nil {
		response(w, 500, "", nil)
		return true
	}
	if !q.Has("path") {
		response(w, 400, "", nil)
		return true
	}
	name, err := managedPath(prefix, q.Get("path"))
	if err != nil {
		response(w, 400, "", nil)
		return true
	}
	if info, err := root.Stat(name); err != nil || !info.Mode().IsRegular() {
		referenceNotFound(w)
		return true
	}
	data, err := body(r, 4095)
	if err != nil {
		response(w, 413, "", nil)
		return true
	}
	form, err := url.ParseQuery(string(data))
	if err != nil {
		response(w, 400, "", nil)
		return true
	}
	if err = setListened(r.Context(), root, name, form.Get("listened") == "true"); err != nil {
		response(w, 500, "", nil)
	} else {
		response(w, 200, "application/octet-stream", []byte("OK"))
	}
	return true
}

// A sync is a download heuristic, not evidence that a person played the audio.
func (s *Server) markLibraryListened(ctx context.Context, snap config.Snapshot, uid string) {
	if !snap.Bool("cloud.autoMarkListenedOnSync") {
		return
	}
	metadata, err := s.Content.Metadata(snap, uid)
	if err != nil || metadata.String("source") == "" {
		return
	}
	global := s.Settings.Snapshot("")
	root, name, err := s.Content.SourceRoot(snap, global, metadata.String("source"))
	if err != nil {
		return
	}
	// A playlist source is not a directly assigned TAF.
	file, err := root.Open(name)
	if err != nil {
		return
	}
	defer file.Close()
	if _, err := taf.ReadHeader(file); err != nil {
		return
	}
	if err := setListened(ctx, root, name, true); err != nil {
		// Sidecar paths and raw OS errors can contain deployment details.
		slog.Warn("could not persist automatic listened status")
	}
}
