// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"goteddycloud/internal/config"
)

const (
	roleAdmin  = "admin"
	roleEditor = "editor"
	roleReader = "reader"
)

func validRole(role string) bool {
	return role == roleAdmin || role == roleEditor || role == roleReader
}
func validateAccess(users []webUser) error {
	admins := 0
	owners := map[string]bool{}
	areas := map[string]bool{}
	for _, user := range users {
		if user.AreaID == "" || areas[user.AreaID] {
			return errors.New("invalid or duplicate area identity")
		}
		areas[user.AreaID] = true
		if !validRole(user.Role) {
			return errors.New("unknown user role")
		}
		if user.Role == roleAdmin {
			admins++
		}
		for _, box := range user.Boxes {
			key := strings.ToLower(box)
			if !config.ValidBoxID(box) || owners[key] {
				return errors.New("invalid or multiply assigned box")
			}
			owners[key] = true
		}
	}
	if len(users) > 0 && admins == 0 {
		return errors.New("at least one administrator is required")
	}
	return nil
}
func publicProfile(user webUser) map[string]any {
	boxes := append([]string{}, user.Boxes...)
	return map[string]any{"username": user.Username, "role": user.Role, "area": user.Username, "defaultArea": user.AreaID, "boxes": boxes}
}
func (s *Server) requestUser(r *http.Request) (webUser, bool) {
	s.auth.mu.Lock()
	defer s.auth.mu.Unlock()
	name := s.auth.sessionUserLocked(requestWebToken(r), s.Now())
	index := s.auth.findUserLocked(name)
	if index < 0 {
		return webUser{}, false
	}
	user := s.auth.users[index]
	user.Boxes = slices.Clone(user.Boxes)
	return user, true
}
func (s *Server) userByName(name string) (webUser, bool) {
	s.auth.mu.Lock()
	defer s.auth.mu.Unlock()
	index := s.auth.findUserLocked(name)
	if index < 0 {
		return webUser{}, false
	}
	user := s.auth.users[index]
	user.Boxes = slices.Clone(user.Boxes)
	return user, true
}
func (s *Server) boxOwner(id string) (webArea, bool) {
	s.auth.mu.Lock()
	defer s.auth.mu.Unlock()
	for _, area := range s.auth.areas {
		for _, box := range area.Boxes {
			if strings.EqualFold(box, id) {
				return cloneArea(area), true
			}
		}
	}
	return webArea{}, false
}

// accessAPI is reached only through the root management authorization gate.
func (s *Server) accessAPI(w http.ResponseWriter, r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/api/auth/") {
		w.Header().Set("Cache-Control", "no-store")
	}
	if s.catalogSharesAPI(w, r) {
		return true
	}
	if s.areaMembersAPI(w, r) {
		return true
	}
	switch r.URL.Path {
	case "/api/auth/profile":
		if r.Method != "GET" {
			response(w, 405, "", nil)
			return true
		}
		user, ok := s.requestUser(r)
		if !ok {
			authFailure(401, "unauthorized", "Login required").write(w, r)
		} else {
			profile := s.profile(user)
			selected := s.sessionArea(r)
			if selected == "" && user.Role != roleAdmin {
				selected = user.AreaID
			}
			profile["selectedArea"] = selected
			area, _ := s.resolveArea(selected)
			profile["areaRole"] = area.role(user.Username)
			if user.Role == roleAdmin {
				profile["areaRole"] = roleAdmin
			}
			jsonReply(w, "application/json", profile)
		}
	case "/api/auth/area":
		if r.Method != "POST" {
			response(w, 405, "", nil)
			return true
		}
		data, err := body(r, maxAuthBodyBytes)
		var payload struct {
			Area string `json:"area"`
		}
		if err != nil || json.Unmarshal(data, &payload) != nil {
			authFailure(400, "invalid_body", "Area required").write(w, r)
			return true
		}
		user, logged := s.requestUser(r)
		admin := !s.authConfigured() || (logged && user.Role == roleAdmin)
		if payload.Area == "" && !admin {
			payload.Area = user.AreaID
		}
		if payload.Area != "" {
			area, ok := s.resolveArea(payload.Area)
			if !ok || (!admin && area.role(user.Username) == "") {
				authFailure(403, "forbidden", "Area membership required").write(w, r)
				return true
			}
			payload.Area = area.ID
		}
		s.auth.mu.Lock()
		for index := range s.auth.sessions {
			if s.auth.sessions[index].token == requestWebToken(r) {
				s.auth.sessions[index].area = payload.Area
			}
		}
		s.auth.mu.Unlock()
		authSuccess(map[string]any{"area": payload.Area}).write(w, r)

	case "/api/auth/users/details":
		if r.Method != "GET" {
			response(w, 405, "", nil)
			return true
		}
		s.auth.mu.Lock()
		profiles := make([]map[string]any, 0, len(s.auth.users))
		for _, user := range s.auth.users {
			profiles = append(profiles, publicProfile(user))
		}
		s.auth.mu.Unlock()
		jsonReply(w, "application/json", map[string]any{"users": profiles})
	case "/api/auth/users/access":
		if r.Method != "POST" {
			response(w, 405, "", nil)
			return true
		}
		data, err := body(r, maxAuthBodyBytes)
		var payload struct {
			Username string    `json:"username"`
			Role     *string   `json:"role"`
			Boxes    *[]string `json:"boxes"`
		}
		if err != nil || json.Unmarshal(data, &payload) != nil || payload.Username == "" {
			authFailure(400, "invalid_body", "Username and valid access fields required").write(w, r)
			return true
		}
		s.auth.mutations.Lock()
		defer s.auth.mutations.Unlock()
		s.auth.mu.Lock()
		users := slices.Clone(s.auth.users)
		index := s.auth.findUserLocked(payload.Username)
		s.auth.mu.Unlock()
		if index < 0 {
			authFailure(404, "not_found", "User not found").write(w, r)
			return true
		}
		if payload.Role != nil {
			users[index].Role = *payload.Role
		}
		if payload.Boxes != nil {
			users[index].Boxes = slices.Clone(*payload.Boxes)
			for _, box := range users[index].Boxes {
				if !s.Settings.HasBox(box) {
					authFailure(400, "invalid_box", "Assign only registered boxes").write(w, r)
					return true
				}
			}
		}
		if err := validateAccess(users); err != nil {
			authFailure(409, "invalid_access", err.Error()).write(w, r)
			return true
		}
		if err := s.saveWebUsers(users); err != nil {
			if errors.Is(err, errAreaAccess) {
				authFailure(409, "invalid_access", err.Error()).write(w, r)
				return true
			}
			authFailure(500, "save_failed", "Could not save access settings").write(w, r)
			return true
		}
		authSuccess(publicProfile(users[index])).write(w, r)
	case "/api/auth/password":
		if r.Method != "POST" {
			response(w, 405, "", nil)
			return true
		}
		user, ok := s.requestUser(r)
		if !ok {
			authFailure(401, "unauthorized", "Login required").write(w, r)
			return true
		}
		data, err := body(r, maxAuthBodyBytes)
		var payload struct {
			CurrentPassword string `json:"currentPassword"`
			Password        string `json:"password"`
		}
		if err != nil || json.Unmarshal(data, &payload) != nil || len(payload.Password) < 4 || len(payload.Password) > maxPasswordBytes || len(payload.CurrentPassword) > maxPasswordBytes {
			authFailure(400, "invalid_body", "Current and new password required").write(w, r)
			return true
		}
		s.auth.mutations.Lock()
		defer s.auth.mutations.Unlock()
		s.auth.mu.Lock()
		index := s.auth.findUserLocked(user.Username)
		valid := index >= 0 && subtle.ConstantTimeCompare([]byte(s.auth.users[index].Hash), []byte(passwordHash(payload.CurrentPassword, s.auth.users[index].Salt))) == 1
		var users []webUser
		if valid {
			users, err = s.auth.changeUserLocked("password", user.Username, payload.Password)
		}
		s.auth.mu.Unlock()
		if !valid {
			authFailure(403, "invalid_password", "Current password is incorrect").write(w, r)
			return true
		}
		if err != nil || s.saveWebUsers(users) != nil {
			authFailure(500, "save_failed", "Could not save password").write(w, r)
			return true
		}
		reply := authSuccess(nil)
		reply.clearCookie = true
		reply.write(w, r)
	default:
		return false
	}
	return true
}

// Close active event streams on any account/access change. Other operations are
// authorized against current state at request entry; in-flight work may complete.
func (s *Server) accessStream(w http.ResponseWriter, r *http.Request, target *Server) {
	s.auth.mu.Lock()
	if s.auth.changed == nil {
		s.auth.changed = make(chan struct{})
	}
	changed := s.auth.changed
	s.auth.mu.Unlock()
	// Capture invalidation before checking permissions again, closing the race
	// between dispatch authorization and establishing a long-lived stream.
	if s.authConfigured() {
		user, ok := s.requestUser(r)
		area, exists := s.areaByID(target.areaID)
		if !ok || (target != s && !exists) || (user.Role != roleAdmin && (target == s || area.role(user.Username) == "")) {
			authFailure(403, "forbidden", "Event stream access revoked").write(w, r)
			return
		}
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go func() {
		select {
		case <-changed:
			cancel()
		case <-ctx.Done():
		}
	}()
	target.sse(w, r.WithContext(ctx))
}
