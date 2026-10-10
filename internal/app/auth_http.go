// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type authReply struct {
	status      int
	payload     map[string]any
	token       string
	clearCookie bool
}

func authFailure(status int, code, message string) authReply {
	return authReply{status: status, payload: map[string]any{"error": code, "message": message}}
}
func authSuccess(payload map[string]any) authReply {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["ok"] = true
	return authReply{status: http.StatusOK, payload: payload}
}
func (reply authReply) write(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	data, err := json.Marshal(reply.payload)
	if err != nil {
		response(w, 500, "", nil)
		return
	}
	if reply.token != "" || reply.clearCookie {
		authCookie(w, r, reply.token, reply.clearCookie)
	}
	response(w, reply.status, "application/json; charset=utf-8", data)
}

var authRoutes = []struct{ method, path, operation string }{
	{method: "GET", path: "status", operation: "status"},
	{method: "GET", path: "users/get", operation: "users"},
	{method: "POST", path: "login", operation: "login"},
	{method: "GET", path: "logout", operation: "logout"},
	{method: "POST", path: "refresh-token", operation: "refresh"},
	{method: "POST", path: "users/create", operation: "create"},
	{method: "POST", path: "users/delete", operation: "delete"},
	{method: "POST", path: "users/updatePassword", operation: "password"},
	{method: "POST", path: "enabled", operation: "enabled"},
}

type authPayload map[string]json.RawMessage

func (payload authPayload) stringField(key string, limit int) (string, bool) {
	raw, present := payload[key]
	var value string
	valid := present && string(raw) != "null" && json.Unmarshal(raw, &value) == nil && len(value) <= limit && !strings.ContainsRune(value, 0)
	return value, valid
}
func (s *Server) authAPI(w http.ResponseWriter, r *http.Request) bool {
	operation := ""
	for _, route := range authRoutes {
		// Prefix matching is an upstream routing contract.
		if strings.EqualFold(r.Method, route.method) && strings.HasPrefix(r.URL.Path, "/api/auth/"+route.path) {
			operation = route.operation
			break
		}
	}
	if operation == "" {
		return false
	}
	var reply authReply
	switch operation {
	case "status", "users", "logout", "refresh":
		reply = s.authRead(operation, r)
	default:
		data, err := body(r, maxAuthBodyBytes)
		var payload authPayload
		if err != nil || json.Unmarshal(data, &payload) != nil || payload == nil {
			message := "Invalid JSON payload"
			if operation == "login" {
				message = "Invalid login payload"
			}
			reply = authFailure(400, "invalid_body", message)
		} else if operation == "login" {
			reply = s.authLogin(payload, r)
		} else {
			reply = s.authMutation(operation, payload)
		}
	}
	// No auth mutex is held while reading a body or writing a response.
	reply.write(w, r)
	return true
}
func (s *Server) authRead(operation string, r *http.Request) authReply {
	configured := s.authConfigured()
	token, now := requestWebToken(r), s.Now()
	s.auth.mu.Lock()
	defer s.auth.mu.Unlock()
	name := s.auth.sessionUserLocked(token, now)
	role, area := "", ""
	if index := s.auth.findUserLocked(name); index >= 0 {
		role, area = s.auth.users[index].Role, s.auth.users[index].Username
	}
	selected, areaRole := "", ""
	for _, session := range s.auth.sessions {
		if session.token == token && name != "" {
			selected = session.area
		}
	}
	if selected == "" && role != roleAdmin {
		if index := s.auth.findUserLocked(name); index >= 0 {
			selected = s.auth.users[index].AreaID
		}
	}
	if index := findArea(s.auth.areas, selected); index >= 0 {
		areaRole = s.auth.areas[index].role(name)
	}
	if role == roleAdmin {
		areaRole = roleAdmin
	}
	enabled := configured && len(s.auth.users) > 0
	switch operation {
	case "status":
		return authReply{status: http.StatusOK, payload: map[string]any{
			"enabled":      enabled,
			"loggedIn":     name != "" && enabled,
			"username":     name,
			"role":         role,
			"area":         area,
			"selectedArea": selected,
			"areaRole":     areaRole,
			"envOverride":  authEnvOverride(),
			"userCount":    len(s.auth.users),
		}}
	case "users":
		names := make([]string, 0, len(s.auth.users))
		for _, user := range s.auth.users {
			names = append(names, user.Username)
		}
		return authReply{status: 200, payload: map[string]any{"users": names, "enabled": enabled, "envOverride": authEnvOverride()}}
	case "logout":
		for index := len(s.auth.sessions) - 1; index >= 0; index-- {
			if s.auth.sessions[index].token == token {
				s.auth.sessions = append(s.auth.sessions[:index], s.auth.sessions[index+1:]...)
			}
		}
		reply := authSuccess(nil)
		reply.clearCookie = true
		return reply
	default:
		if name == "" {
			return authFailure(401, "unauthorized", "Login required")
		}
		return authSuccess(map[string]any{"username": name})
	}
}
func credentials(payload authPayload, operation string) (string, string, authReply, bool) {
	username, valid := payload.stringField("username", maxUsernameBytes)
	password, passValid := payload.stringField("password", maxPasswordBytes)
	if valid && (operation == "delete" || passValid) {
		return username, password, authReply{}, true
	}
	message := "Username and password required"
	if operation == "delete" {
		message = "Username required"
	}
	return "", "", authFailure(400, "invalid_body", message), false
}
func (s *Server) authLogin(payload authPayload, r *http.Request) authReply {
	username, password, failure, valid := credentials(payload, "login")
	if !valid {
		return failure
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	now := s.Now()
	s.auth.mu.Lock()
	defer s.auth.mu.Unlock()
	bucket := s.auth.loginBucketLocked(ip)
	if bucket >= 0 && !s.auth.limits[bucket].until.IsZero() {
		if now.Before(s.auth.limits[bucket].until) {
			return authFailure(429, "rate_limited", "Too many login attempts, try again later")
		}
		s.auth.limits[bucket].failures = 0
		s.auth.limits[bucket].until = time.Time{}
	}
	index := s.auth.findUserLocked(username)
	if index >= 0 && subtle.ConstantTimeCompare([]byte(s.auth.users[index].Hash), []byte(passwordHash(password, s.auth.users[index].Salt))) == 1 {
		name := s.auth.users[index].Username
		token, err := s.auth.newSessionLocked(name, now)
		if err != nil {
			return authFailure(500, "save_failed", "Could not create session")
		}
		if bucket >= 0 {
			s.auth.limits = append(s.auth.limits[:bucket], s.auth.limits[bucket+1:]...)
		}
		reply := authSuccess(map[string]any{"username": name, "token": token, "role": s.auth.users[index].Role, "area": name})
		reply.token = token
		return reply
	}
	s.auth.failedLoginLocked(ip, bucket, now)
	return authFailure(401, "unauthorized", "Invalid username or password")
}
func (auth *webAuth) loginBucketLocked(ip string) int {
	for index, limit := range auth.limits {
		if limit.ip == ip {
			return index
		}
	}
	return -1
}
func (auth *webAuth) failedLoginLocked(ip string, bucket int, now time.Time) {
	if ip == "" {
		return
	}
	if bucket < 0 {
		limit := loginLimit{ip: ip}
		if len(auth.limits) < maxLoginBuckets {
			auth.limits = append(auth.limits, limit)
			bucket = len(auth.limits) - 1
		} else {
			auth.limits[0] = limit
			bucket = 0
		}
	}
	auth.limits[bucket].failures++
	if auth.limits[bucket].failures >= maxLoginFailures {
		auth.limits[bucket].until = now.Add(loginLockout)
	}
}
func (s *Server) authMutation(operation string, payload authPayload) authReply {
	s.auth.mutations.Lock()
	defer s.auth.mutations.Unlock()
	if operation == "enabled" {
		return s.authSetEnabled(payload)
	}
	username, password, failure, valid := credentials(payload, operation)
	if !valid {
		return failure
	}
	s.auth.mu.Lock()
	users, err := s.auth.changeUserLocked(operation, username, password)
	if err == nil && operation == "create" {
		role := roleEditor
		if len(s.auth.users) == 0 {
			role = roleAdmin
		}
		if _, present := payload["role"]; present {
			var valid bool
			role, valid = payload.stringField("role", 16)
			if !valid || !validRole(role) {
				err = errInvalidUser
			}
		}
		users[len(users)-1].Role = role
	}
	s.auth.mu.Unlock()
	if err == nil {
		if accessErr := validateAccess(users); accessErr != nil {
			return authFailure(409, "invalid_access", accessErr.Error())
		}
	}
	if err != nil {
		return userChangeFailure(err)
	}
	if err := s.saveWebUsers(users); err != nil {
		if errors.Is(err, errAreaAccess) {
			return authFailure(409, "invalid_access", err.Error())
		}
		return authFailure(500, "save_failed", "Could not save users")
	}
	if operation == "delete" && len(users) == 0 {
		if err := s.Settings.Set("", "frontend.web_auth_enabled", "false"); err != nil {
			return authFailure(500, "save_failed", "Could not update login setting")
		}
	}
	switch operation {
	case "create":
		return authSuccess(map[string]any{"username": username})
	case "delete":
		return authSuccess(map[string]any{"authDisabled": len(users) == 0})
	default:
		return authSuccess(nil)
	}
}
func (s *Server) authSetEnabled(payload authPayload) authReply {
	var enabled bool
	raw, present := payload["enabled"]
	if !present || string(raw) == "null" || json.Unmarshal(raw, &enabled) != nil {
		return authFailure(400, "invalid_body", "enabled required")
	}
	if authEnvOverride() {
		return authFailure(400, "env_override", "Login protection is disabled by TEDDYCLOUD_WEB_AUTH_DISABLE")
	}
	s.auth.mu.Lock()
	empty := len(s.auth.users) == 0
	s.auth.mu.Unlock()
	if !enabled {
		s.auth.mu.Lock()
		restricted := false
		for _, user := range s.auth.users {
			restricted = restricted || user.Role != roleAdmin
		}
		s.auth.mu.Unlock()
		if restricted {
			return authFailure(409, "private_areas", "Login cannot be disabled while non-admin users exist")
		}
	}
	if enabled && empty {
		return authFailure(400, "no_users", "Create a user before enabling login protection")
	}
	if err := s.Settings.Set("", "frontend.web_auth_enabled", strconv.FormatBool(enabled)); err != nil {
		return authFailure(500, "save_failed", "Could not update login setting")
	}
	return authSuccess(map[string]any{"enabled": enabled})
}

func userChangeFailure(err error) authReply {
	switch {
	case errors.Is(err, errInvalidUser):
		return authFailure(400, "invalid_user", "Invalid username or password")
	case errors.Is(err, errInvalidPassword):
		return authFailure(400, "invalid_user", "Invalid password")
	case errors.Is(err, errUserExists):
		return authFailure(409, "exists", "User already exists")
	case errors.Is(err, errUserLimit):
		return authFailure(400, "limit", "Maximum number of users reached")
	case errors.Is(err, errUserNotFound):
		return authFailure(404, "not_found", "User not found")
	default:
		return authFailure(500, "save_failed", "Could not save users")
	}
}
