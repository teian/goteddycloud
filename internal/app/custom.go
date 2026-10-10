// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"goteddycloud/internal/catalog"
)

func (s *Server) customAPI(w http.ResponseWriter, r *http.Request) bool {
	if !strings.EqualFold(r.Method, "POST") {
		return false
	}
	upsert := strings.HasPrefix(r.URL.Path, "/api/toniesCustomJsonUpsert")
	remove := strings.HasPrefix(r.URL.Path, "/api/toniesCustomJsonDelete")
	rename := strings.HasPrefix(r.URL.Path, "/api/toniesCustomJsonRename")
	if !upsert && !remove && !rename {
		return false
	}
	reply := func(status int, message string) { response(w, status, "text/plain; charset=utf-8", []byte(message)) }
	data, err := body(r, 1<<20)
	if err != nil || len(data) == 0 {
		reply(400, "Invalid body size")
		return true
	}
	if !json.Valid(data) {
		reply(400, "Invalid JSON payload")
		return true
	}
	keep := s.Settings.Snapshot("").Uint("tonie_json.custom_backup_keep")
	if rename {
		var object map[string]json.RawMessage
		if bytes.TrimSpace(data)[0] != '{' || json.Unmarshal(data, &object) != nil {
			reply(400, "Invalid payload: expected object")
			return true
		}
		var from, to string
		_ = json.Unmarshal(object["fromModel"], &from)
		_ = json.Unmarshal(object["toModel"], &to)
		if from == "" || to == "" {
			reply(400, "Invalid payload: 'fromModel' and 'toModel' are required")
			return true
		}
		linked := true
		if raw, ok := object["updateContentJson"]; ok && (string(raw) == "false" || string(raw) == "true") {
			_ = json.Unmarshal(raw, &linked)
		}
		snap := s.Settings.Snapshot("")
		err = s.Catalog.Rename(from, to, snap.String("internal.contentdirfull"), linked, keep, s.Now(), func(from, to string) error { return s.Content.RenameModels(r.Context(), snap, from, to) })
	} else if upsert {
		var entries []catalog.Entry
		trimmed := bytes.TrimSpace(data)
		switch trimmed[0] {
		case '{':
			var entry catalog.Entry
			_ = json.Unmarshal(trimmed, &entry)
			entries = []catalog.Entry{entry}
		case '[':
			var rows []json.RawMessage
			_ = json.Unmarshal(trimmed, &rows)
			for _, row := range rows {
				var entry catalog.Entry
				_ = json.Unmarshal(row, &entry)
				entries = append(entries, entry)
			}
		default:
			reply(400, "Invalid payload: expected object or array")
			return true
		}
		err = s.Catalog.Upsert(entries, keep, s.Now())
	} else {
		trimmed := bytes.TrimSpace(data)
		var rows []json.RawMessage
		if trimmed[0] == '[' {
			_ = json.Unmarshal(trimmed, &rows)
		} else if trimmed[0] == '{' {
			var object map[string]json.RawMessage
			_ = json.Unmarshal(trimmed, &object)
			_ = json.Unmarshal(object["models"], &rows)
		}
		if len(rows) == 0 {
			reply(400, "Invalid payload: 'models' array is required")
			return true
		}
		var models []string
		for _, row := range rows {
			var model string
			if len(row) > 0 && row[0] == '"' && json.Unmarshal(row, &model) == nil {
				models = append(models, model)
			}
		}
		err = s.Catalog.Delete(models, keep, s.Now())
	}
	if err != nil {
		status, message := customMutationResponse(err)
		reply(status, message)
	} else {
		s.registerCatalogImages(r.Context())
		reply(200, "OK")
	}
	return true
}

func customMutationResponse(err error) (int, string) {
	var mutation *catalog.MutationError
	if !errors.As(err, &mutation) {
		return http.StatusBadRequest, err.Error()
	}
	status := http.StatusBadRequest
	switch mutation.Kind {
	case catalog.CatalogReadFailure, catalog.CatalogBackupFailure, catalog.RenamePending, catalog.RenameRecoveryFailure:
		status = http.StatusInternalServerError
	case catalog.MissingModel:
		status = http.StatusNotFound
	}
	// Retain the C reference's 400 response for encode/save/journal failures.
	return status, mutation.ReferenceMessage()
}
