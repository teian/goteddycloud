// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"goteddycloud/internal/boxcommands"
	"goteddycloud/internal/config"
)

func (s *Server) settingsAPI(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	query := r.URL.Query()
	id := query.Get("overlay")
	snap := s.Settings.Snapshot(id)
	get := strings.EqualFold(r.Method, "GET")
	post := strings.EqualFold(r.Method, "POST")
	switch {
	case get && strings.HasPrefix(path, "/api/settings/getIndex"):
		includeInternal := strings.HasPrefix(query.Get("internal"), "t")
		ignoreLevel := strings.HasPrefix(query.Get("nolevel"), "t")
		level := snap.Uint("core.settings_level")
		options := []map[string]any{}
		for _, d := range config.Definitions() {
			if d.Level == 99 {
				continue
			}
			if d.Internal && !includeInternal {
				continue
			}
			if !ignoreLevel && uint64(d.Level) > level {
				continue
			}
			row := map[string]any{"ID": d.ID, "shortname": d.ID, "description": d.Description, "label": d.Label, "overlayed": s.Settings.IsOverlayed(id, d.ID), "internal": d.Internal, "level": d.Level}
			if d.Type != "" {
				row["type"] = d.Type
				row["valueInit"] = d.Default
				row["value"] = settingValue(d, snap.String(d.ID))
			}
			if d.Type == "int" || d.Type == "uint" || d.Type == "hex" || d.Type == "float" {
				row["min"] = d.Min
				row["max"] = d.Max
			}
			options = append(options, row)
		}
		data, _ := json.Marshal(map[string]any{"options": options})
		response(w, 200, "text/json", data)
	case get && strings.HasPrefix(path, "/api/settings/get/"):
		key := strings.TrimPrefix(path, "/api/settings/get/")
		d, ok := s.Settings.Definition(key)
		if !ok {
			http.NotFound(w, r)
			return true
		}
		value := snap.String(key)
		if d.Type == "float" {
			v, _ := strconv.ParseFloat(value, 64)
			value = strconv.FormatFloat(v, 'f', 6, 64)
		}
		if d.Level == 99 || d.Type == "" {
			value = "ERROR"
		}
		response(w, 200, "text/plain", []byte(value))
	case post && strings.HasPrefix(path, "/api/settings/set/"):
		data, err := body(r, 4095)
		if err == nil && len(data) > 0 {
			key := strings.TrimPrefix(path, "/api/settings/set/")
			if mapped, _, _, _ := boxcommands.Setting(key); id != "" && mapped != "" {
				_ = s.Commands.Execute(id, key, string(data))
			} else {
				_ = s.Settings.Set(id, key, string(data))
			}
		}
		response(w, 200, "text/plain; charset=utf-8", nil)
	case post && strings.HasPrefix(path, "/api/settings/reset/"):
		key := strings.TrimPrefix(path, "/api/settings/reset/")
		value := "ERROR"
		if (id == "" || s.Settings.IsOverlayed(id, key)) && s.Settings.Reset(id, key) == nil {
			value = "OK"
		}
		response(w, 200, "text/plain; charset=utf-8", []byte(value))
	case post && strings.HasPrefix(path, "/api/settings/removeOverlay"):
		known := false
		for _, box := range s.Settings.BoxIDs() {
			known = known || box == id
		}
		if !known || s.Settings.RemoveOverlay(id) != nil {
			response(w, 400, "", nil)
			return true
		}
		response(w, 200, "application/octet-stream", []byte("OK"))
	case get && strings.HasPrefix(path, "/api/getBoxes"):
		boxes := []map[string]string{}
		for _, id := range s.Settings.BoxIDs() {
			snap := s.Settings.Snapshot(id)
			boxes = append(boxes, map[string]string{"ID": id, "commonName": snap.String("commonName"), "boxName": snap.String("boxName"), "boxModel": snap.String("boxModel")})
		}
		data, _ := json.Marshal(map[string]any{"boxes": boxes})
		response(w, 200, "text/json", data)
	default:
		return false
	}
	return true
}
func settingValue(d config.Definition, raw string) any {
	switch d.Type {
	case "bool":
		return raw == "true"
	case "float":
		v, _ := strconv.ParseFloat(raw, 64)
		return v
	case "int", "uint", "hex":
		v, _ := strconv.ParseInt(raw, 10, 64)
		return v
	default:
		return raw
	}
}
