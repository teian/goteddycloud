// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
)

// Only global administrators create/delete areas and assign registered devices.
// Area administrators may rename their area and manage its existing-user members.
func (s *Server) areaMembersAPI(w http.ResponseWriter, r *http.Request) bool {
	switch r.URL.Path {
	case "/api/auth/areas", "/api/auth/areas/create", "/api/auth/areas/update", "/api/auth/areas/delete":
	default:
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	user, logged := s.requestUser(r)
	admin := !s.authConfigured() || (logged && user.Role == roleAdmin)
	if r.URL.Path == "/api/auth/areas" {
		if r.Method != "GET" {
			response(w, 405, "", nil)
			return true
		}
		var areas []webArea
		if admin {
			s.auth.mu.Lock()
			areas = cloneAreas(s.auth.areas)
			s.auth.mu.Unlock()
		} else {
			areas = s.accessibleAreas(user)
		}
		if areas == nil {
			areas = []webArea{}
		}
		jsonReply(w, "application/json", map[string]any{"areas": areas})
		return true
	}
	if r.Method != "POST" {
		response(w, 405, "", nil)
		return true
	}
	data, err := body(r, maxAuthBodyBytes)
	var payload struct {
		Area    string        `json:"area"`
		Name    *string       `json:"name"`
		Members *[]areaMember `json:"members"`
		Boxes   *[]string     `json:"boxes"`
	}
	if err != nil || json.Unmarshal(data, &payload) != nil {
		authFailure(400, "invalid_body", "Valid area payload required").write(w, r)
		return true
	}
	s.auth.mutations.Lock()
	defer s.auth.mutations.Unlock()
	// Resolve selectors and recheck current privileges while mutations are serialized.
	user, logged = s.requestUser(r)
	admin = !s.authConfigured() || (logged && user.Role == roleAdmin)
	s.auth.mu.Lock()
	users := slices.Clone(s.auth.users)
	areas := cloneAreas(s.auth.areas)
	s.auth.mu.Unlock()
	var record webArea
	index := -1
	if r.URL.Path == "/api/auth/areas/create" {
		if !admin {
			authFailure(403, "forbidden", "Global administrator required").write(w, r)
			return true
		}
		id, err := newAreaID()
		if err != nil {
			authFailure(500, "save_failed", "Could not create area identity").write(w, r)
			return true
		}
		record = webArea{ID: id, Boxes: []string{}, Members: []areaMember{}}
		if logged {
			record.Members = append(record.Members, areaMember{Username: user.Username, Role: roleAdmin})
		}
	} else {
		var exists bool
		record, exists = s.resolveArea(payload.Area)
		if !exists || (!admin && record.role(user.Username) != roleAdmin) {
			authFailure(403, "forbidden", "Area administrator required").write(w, r)
			return true
		}
		index = findArea(areas, record.ID)
	}
	if r.URL.Path == "/api/auth/areas/delete" {
		if !admin {
			authFailure(403, "forbidden", "Global administrator required").write(w, r)
			return true
		}
		for _, account := range users {
			if account.AreaID == record.ID {
				authFailure(409, "default_area", "An account's default area cannot be deleted").write(w, r)
				return true
			}
		}
		areas = slices.Delete(areas, index, index+1)
	} else {
		if payload.Name != nil {
			record.Name = *payload.Name
		}
		if payload.Members != nil {
			record.Members = slices.Clone(*payload.Members)
			for i, member := range record.Members {
				if account := findUser(users, member.Username); account >= 0 {
					record.Members[i].Username = users[account].Username
				}
			}
		}
		if payload.Boxes != nil {
			if !admin {
				authFailure(403, "forbidden", "Only global administrators assign boxes").write(w, r)
				return true
			}
			record.Boxes = slices.Clone(*payload.Boxes)
			for _, box := range record.Boxes {
				if !s.Settings.HasBox(box) {
					authFailure(400, "invalid_box", "Assign only registered boxes").write(w, r)
					return true
				}
			}
		}
		if index < 0 {
			areas = append(areas, record)
		} else {
			areas[index] = record
		}
	}
	if err := validateAreas(users, areas); err != nil {
		authFailure(409, "invalid_access", err.Error()).write(w, r)
		return true
	}
	if err := s.saveAccess(users, areas); err != nil {
		if errors.Is(err, errAreaAccess) {
			authFailure(409, "invalid_access", err.Error()).write(w, r)
		} else {
			authFailure(500, "save_failed", "Could not save area settings").write(w, r)
		}
		return true
	}
	reply := map[string]any{"area": record}
	if strings.HasSuffix(r.URL.Path, "/delete") {
		reply = map[string]any{"deleted": record.ID}
	}
	authSuccess(reply).write(w, r)
	return true
}
