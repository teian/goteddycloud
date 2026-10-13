// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"net/http"
	"slices"
	"strings"

	"goteddycloud/internal/config"
)

// Authorization is an exact allowlist; upstream prefix routes cannot expand it.
func areaRoute(r *http.Request, write bool) bool {
	p := r.URL.Path
	if r.Method == "GET" {
		switch p {
		case "/api/time", "/api/getBoxes", "/api/getTagInfo", "/api/getTagIndex", "/api/fileIndex", "/api/fileIndexV2", "/api/toniesJson", "/api/toniesJsonSearch", "/api/toniesCustomJson", "/api/tonieboxesJson", "/api/tonieboxesCustomJson", "/api/stats", "/api/sse":
			return true
		}
		return strings.HasPrefix(p, "/content/") || strings.HasPrefix(p, "/custom_img/") || strings.HasPrefix(p, "/cache/")
	}
	if write && r.Method == "POST" {
		switch p {
		case "/api/fileUpload", "/api/fileDelete", "/api/fileMove", "/api/dirDelete", "/api/dirCreate", "/api/migrateContent2Lib", "/api/fileSetListened", "/api/assignUnknown", "/api/tafUpload", "/api/pcmUpload", "/api/fileEncode", "/api/toniesCustomJsonUpsert", "/api/toniesCustomJsonDelete", "/api/toniesCustomJsonRename":
			return true
		}
		return strings.HasPrefix(p, "/content/json/set/")
	}
	return false
}
func privateBoxSetting(key string) bool {
	switch key {
	case "toniebox.max_vol_spk", "toniebox.max_vol_hdp", "toniebox.slap_enabled", "toniebox.slap_back_left", "toniebox.led", "toniebox2.baby_mode", "toniebox2.lightring_brightness":
		return true
	}
	return false
}
func (s *Server) managementAccess(w http.ResponseWriter, r *http.Request) bool {
	user, logged := s.requestUser(r)
	admin := !s.authConfigured() || (logged && user.Role == roleAdmin)
	p := r.URL.Path
	for _, segment := range strings.Split(p, "/") {
		if segment == "." || segment == ".." {
			authFailure(400, "invalid_path", "Non-canonical path").write(w, r)
			return true
		}
	}
	deny := func() bool {
		authFailure(403, "forbidden", "Insufficient permissions for this area or operation").write(w, r)
		return true
	}
	if publicWebPath(p) || (p == "/tools/users" || p == "/tools/shares" || p == "/tools/shares.mjs" || p == "/plugins/goteddycloud-users/index.html" || p == "/plugins/goteddycloud-users/") {
		return false
	}
	if strings.HasPrefix(p, "/api/auth/") {
		if admin || catalogShareRoute(p) || p == "/api/auth/password" || p == "/api/auth/profile" || p == "/api/auth/area" || p == "/api/auth/areas" || p == "/api/auth/areas/update" {
			return false
		}
		return deny()
	}
	if !admin && p == "/api/plugins/get" && r.Method == "GET" {
		jsonReply(w, "application/json", []string{})
		return true
	}
	if !admin && !logged {
		return deny()
	}
	// Global connectivity is safe for all authenticated roles, independently
	// of the selected content area. The handler exposes no identity material.
	if p == "/api/cloud/status" && r.Method == http.MethodGet {
		return false
	}
	query := r.URL.Query()
	if len(query["area"]) > 1 || len(query["overlay"]) > 1 {
		return deny()
	}
	if admin && (strings.HasPrefix(p, "/plugins/") || p == "/api/plugins/get" || strings.HasPrefix(p, "/tools/")) {
		return false
	}
	if admin {
		if p == "/api/settings/getIndex" && query.Get("overlay") == "" {
			return false
		}
		for _, prefix := range []string{"/api/settings/get/", "/api/settings/set/", "/api/settings/reset/"} {
			if strings.HasPrefix(p, prefix) && !privateBoxSetting(strings.TrimPrefix(p, prefix)) {
				return false
			}
		}
	}
	selected := query.Get("area")
	if _, explicit := query["area"]; !explicit {
		selected = s.sessionArea(r)
	}
	if selected == "" && !admin {
		selected = user.AreaID
	}
	if selected == "" && admin {
		if owner, ok := s.boxOwner(query.Get("overlay")); ok {
			selected = owner.ID
		}
	}
	if selected == "" {
		return false
	}
	record, ok := s.resolveArea(selected)
	if !ok {
		return deny()
	}
	areaRole := record.role(user.Username)
	if !admin && areaRole == "" {
		return deny()
	}
	overlay := query.Get("overlay")
	if overlay != "" && !slices.Contains(record.Boxes, overlay) {
		return deny()
	}
	write := admin || areaRole == roleAdmin || areaRole == roleEditor
	settingKey := ""
	for _, prefix := range []string{"/api/settings/get/", "/api/settings/set/", "/api/settings/reset/"} {
		if strings.HasPrefix(p, prefix) {
			settingKey = strings.TrimPrefix(p, prefix)
		}
	}
	boxSetting := overlay != "" && privateBoxSetting(settingKey) && ((r.Method == "GET" && strings.HasPrefix(p, "/api/settings/get/")) || (write && r.Method == "POST" && (strings.HasPrefix(p, "/api/settings/set/") || strings.HasPrefix(p, "/api/settings/reset/"))))
	index := r.Method == "GET" && p == "/api/settings/getIndex"
	if !areaRoute(r, write) && !boxSetting && !index {
		if admin {
			return false
		}
		return deny()
	}
	target, err := s.areaServer(record)
	if err != nil {
		authFailure(503, "area_unavailable", "User area unavailable").write(w, r)
		return true
	}
	if index {
		options := []map[string]any{}
		if overlay != "" {
			for _, d := range config.Definitions() {
				if !d.Internal && d.Type != "" && privateBoxSetting(d.ID) {
					options = append(options, map[string]any{"ID": d.ID, "shortname": d.ID, "label": d.Label, "description": d.Description, "type": d.Type, "value": settingValue(d, target.Settings.String(overlay, d.ID)), "valueInit": d.Default, "min": d.Min, "max": d.Max})
				}
			}
		}
		jsonReply(w, "application/json", map[string]any{"options": options})
		return true
	}
	if strings.HasPrefix(p, "/content/") || strings.HasPrefix(p, "/custom_img/") {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	}
	if p == "/api/sse" {
		s.accessStream(w, r, target)
	} else {
		target.serveManagement(w, r)
	}
	return true
}
func (s *Server) sessionArea(r *http.Request) string {
	s.auth.mu.Lock()
	defer s.auth.mu.Unlock()
	for _, session := range s.auth.sessions {
		if session.token == requestWebToken(r) && s.Now().Before(session.expires) {
			return session.area
		}
	}
	return ""
}
