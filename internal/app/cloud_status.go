// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// A status probe succeeding does not mean the external cloud is available.
// Keep ordinary offline/identity failures out of the browser's HTTP error path.
func (s *Server) cloudStatus(w http.ResponseWriter, r *http.Request) {
	snap := s.Settings.Snapshot("")
	status := struct {
		Enabled   bool `json:"enabled"`
		Available bool `json:"available"`
	}{Enabled: snap.Bool("cloud.enabled")}
	if status.Enabled {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		// Web cookies and bearer tokens must never become cloud credentials.
		res, err := s.doCloud(ctx, snap, snap, http.MethodGet, "/v1/time", "", nil, nil)
		if err == nil && res != nil && res.Body != nil {
			defer res.Body.Close()
			data, readErr := io.ReadAll(io.LimitReader(res.Body, 65))
			epoch, parseErr := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
			status.Available = res.StatusCode == http.StatusOK && readErr == nil && len(data) <= 64 && parseErr == nil && epoch > 0
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonReply(w, "application/json; charset=utf-8", status)
}
