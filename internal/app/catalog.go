// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

func (s *Server) catalogAPI(w http.ResponseWriter, r *http.Request) bool {
	if s.customAPI(w, r) {
		return true
	}
	if !strings.EqualFold(r.Method, "GET") {
		return false
	}
	switch {
	case strings.HasPrefix(r.URL.Path, "/api/toniesJsonSearch"):
		query := r.URL.Query()
		rows := s.Catalog.Search(query.Get("searchModel"), query.Get("searchSeries"), query.Get("searchEpisode"))
		for _, row := range rows {
			row["picture"] = s.registerImage(r.Context(), row["picture"].(string), s.Settings.Snapshot("").Bool("tonie_json.cache_preload"), false)
		}
		data, _ := json.Marshal(rows)
		response(w, 200, "text/json", data)
	case strings.HasPrefix(r.URL.Path, "/api/toniesJsonUpdate"):
		response(w, 200, "text/plain; charset=utf-8", []byte("Triggered tonies.json update"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if err := s.updateCatalog(r.Context()); err != nil {
			slog.Warn("metadata update failed; previous catalog retained", "error", err)
		}
	case strings.HasPrefix(r.URL.Path, "/api/toniesJsonReload"):
		if err := s.Catalog.Reload(); err != nil {
			response(w, 500, "text/plain; charset=utf-8", []byte("metadata reload failed"))
			return true
		}
		s.registerCatalogImages(r.Context())
		response(w, 200, "text/plain; charset=utf-8", []byte("OK"))
	case strings.HasPrefix(r.URL.Path, "/api/toniesCustomJson"):
		entries, err := s.Catalog.ReadCustom()
		if err != nil {
			response(w, 200, "application/json; charset=utf-8", []byte("[]"))
			return true
		}
		data, _ := json.Marshal(entries)
		response(w, 200, "text/json", data)
	case r.URL.Path == "/api/toniesJson":
		data, err := s.Catalog.ReadOfficial()
		if err != nil {
			response(w, 404, "", nil)
			return true
		}
		response(w, 200, "application/octet-stream", data)
	default:
		return false
	}
	return true
}
