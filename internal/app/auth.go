// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookie     = "tc_web_session"
	sessionTTL        = 7 * 24 * time.Hour
	maxWebUsers       = 32
	maxWebSessions    = 64
	maxLoginBuckets   = 32
	maxLoginFailures  = 5
	loginLockout      = 10 * time.Minute
	maxAuthBodyBytes  = 64 << 10
	maxUsernameBytes  = 32
	maxPasswordBytes  = 128
	passwordSaltBytes = 16
	sessionTokenBytes = 32
)

type webUser struct {
	Username string   `json:"username"`
	Salt     string   `json:"salt"`
	Hash     string   `json:"hash"`
	Role     string   `json:"role"`
	AreaID   string   `json:"areaId"`
	Boxes    []string `json:"boxes,omitempty"`
}
type webUsersFile struct {
	Users []webUser `json:"users"`
	Areas []webArea `json:"areas"`
}
type webSession struct {
	token, username string
	area            string
	expires         time.Time
}
type loginLimit struct {
	ip       string
	failures int
	until    time.Time
}

// mutations serializes durable changes without blocking authentication reads.
// mu protects in-memory state only. Methods ending in Locked require mu.
type webAuth struct {
	mutations sync.Mutex
	mu        sync.Mutex
	users     []webUser
	areas     []webArea
	sessions  []webSession
	limits    []loginLimit
	changed   chan struct{}
}

func (s *Server) loadWebUsers() error {
	s.auth.mutations.Lock()
	defer s.auth.mutations.Unlock()
	root, err := s.Settings.Storage().OpenRoot(s.Settings.String("", "internal.configdirfull"))
	if err != nil {
		return err
	}
	defer root.Close()
	file, err := root.Open("web_users.json")
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxAuthBodyBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxAuthBodyBytes {
		return errors.New("web users file exceeds 64 KiB")
	}
	var stored webUsersFile
	if err := json.Unmarshal(data, &stored); err != nil {
		return fmt.Errorf("decode web users: %w", err)
	}
	if stored.Users == nil {
		return errors.New("web users file requires a users array")
	}
	if len(stored.Users) > maxWebUsers {
		return errors.New("web users file exceeds user limit")
	}
	seen := make(map[string]bool, len(stored.Users))
	for index, user := range stored.Users {
		salt, saltErr := hex.DecodeString(user.Salt)
		hash, hashErr := hex.DecodeString(user.Hash)
		name := strings.ToLower(user.Username)
		if !validWebUsername(user.Username) || saltErr != nil || hashErr != nil || len(salt) != passwordSaltBytes || len(hash) != sha256.Size || seen[name] {
			return fmt.Errorf("invalid or duplicate web user at index %d", index)
		}
		if user.AreaID == "" {
			stored.Users[index].AreaID = user.Salt
		}
		if area, err := hex.DecodeString(stored.Users[index].AreaID); err != nil || len(area) != passwordSaltBytes {
			return errors.New("invalid area ID")
		}
		if user.Role == "" {
			stored.Users[index].Role = roleAdmin
		}
		if !validRole(stored.Users[index].Role) {
			return fmt.Errorf("invalid role for web user at index %d", index)
		}
		seen[name] = true
	}
	if err := validateAccess(stored.Users); err != nil {
		return err
	}
	if stored.Areas == nil {
		stored.Areas = reconcileUserAreas(nil, stored.Users, nil)
	}
	if err := validateAreas(stored.Users, stored.Areas); err != nil {
		return err
	}
	// Validate everything before replacing any live state.
	s.auth.mu.Lock()
	s.auth.users = stored.Users
	s.auth.areas = stored.Areas
	s.auth.mu.Unlock()
	return nil
}

// saveWebUsers requires mutations. Disk I/O never holds the state mutex;
// readers see the previous users until the durable replacement succeeds.
func (s *Server) saveWebUsers(users []webUser) error {
	s.auth.mu.Lock()
	areas := reconcileUserAreas(s.auth.users, users, s.auth.areas)
	s.auth.mu.Unlock()
	return s.saveAccess(users, areas)
}

// saveAccess publishes users and memberships atomically in the same storage object.
// The caller must hold auth.mutations across snapshot, validation and publication.
func (s *Server) saveAccess(users []webUser, areas []webArea) error {
	if err := validateAreas(users, areas); err != nil {
		return err
	}
	users = slices.Clone(users)
	for i := range users {
		if index := findArea(areas, users[i].AreaID); index >= 0 {
			users[i].Boxes = slices.Clone(areas[index].Boxes)
		}
	}
	if err := validateAccess(users); err != nil {
		return err
	}
	root, err := s.Settings.Storage().OpenRoot(s.Settings.String("", "internal.configdirfull"))
	if err != nil {
		return err
	}
	defer root.Close()
	data, err := json.Marshal(webUsersFile{Users: users, Areas: areas})
	if err != nil {
		return err
	}
	if len(data) > maxAuthBodyBytes {
		return fmt.Errorf("%w: user/area database exceeds 64 KiB", errAreaAccess)
	}
	if err := root.WriteAtomic("web_users.json", data, 0600); err != nil {
		return err
	}
	s.auth.mu.Lock()
	// Invalidate deleted users and changed passwords. Role/box changes are read
	// from current user state on every request, never copied into a token.
	sessions := s.auth.sessions[:0]
	for _, session := range s.auth.sessions {
		oldIndex := s.auth.findUserLocked(session.username)
		for _, user := range users {
			if oldIndex >= 0 && strings.EqualFold(user.Username, session.username) && user.Hash == s.auth.users[oldIndex].Hash && user.Salt == s.auth.users[oldIndex].Salt {
				sessions = append(sessions, session)
				break
			}
		}
	}
	s.auth.sessions = sessions
	s.auth.users = users
	s.auth.areas = areas
	if s.auth.changed != nil {
		close(s.auth.changed)
	}
	s.auth.changed = make(chan struct{})
	s.auth.mu.Unlock()
	return nil
}
func validWebUsername(name string) bool {
	if len(name) < 1 || len(name) > maxUsernameBytes {
		return false
	}
	for _, char := range name {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' || char == '.') {
			return false
		}
	}
	return true
}
func (auth *webAuth) findUserLocked(name string) int {
	for index, user := range auth.users {
		if strings.EqualFold(user.Username, name) {
			return index
		}
	}
	return -1
}
func passwordHash(password, salt string) string {
	// Salts enter state only after validation or generation.
	decoded, _ := hex.DecodeString(salt)
	digest := sha256.Sum256(append(decoded, []byte(password)...))
	return hex.EncodeToString(digest[:])
}
func saltedUser(name, password string) (webUser, error) {
	saltBytes := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(saltBytes); err != nil {
		return webUser{}, err
	}
	salt := hex.EncodeToString(saltBytes)
	return webUser{Username: name, Salt: salt, Hash: passwordHash(password, salt), Role: roleAdmin, AreaID: salt}, nil
}
func authEnvOverride() bool {
	value := os.Getenv("TEDDYCLOUD_WEB_AUTH_DISABLE")
	return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
}
func (s *Server) authConfigured() bool {
	if authEnvOverride() {
		return false
	}
	s.auth.mu.Lock()
	restricted := false
	for _, user := range s.auth.users {
		restricted = restricted || user.Role != roleAdmin
	}
	s.auth.mu.Unlock()
	return restricted || s.Settings.Bool("", "frontend.web_auth_enabled")
}
func requestWebToken(r *http.Request) string {
	if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok && len(token) > 0 && len(token) <= sessionTokenBytes*2 {
		return token
	}
	if cookie, err := r.Cookie(sessionCookie); err == nil && len(cookie.Value) <= sessionTokenBytes*2 {
		return cookie.Value
	}
	return ""
}
func (auth *webAuth) sessionUserLocked(token string, now time.Time) string {
	if token == "" {
		return ""
	}
	for _, session := range auth.sessions {
		if auth.findUserLocked(session.username) >= 0 && !now.After(session.expires) && subtle.ConstantTimeCompare([]byte(session.token), []byte(token)) == 1 {
			return session.username
		}
	}
	return ""
}
func (auth *webAuth) newSessionLocked(name string, now time.Time) (string, error) {
	token := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}
	session := webSession{token: hex.EncodeToString(token), username: name, expires: now.Add(sessionTTL)}
	slot := -1
	for index, old := range auth.sessions {
		if now.After(old.expires) {
			slot = index
			break
		}
		if slot < 0 || old.expires.Before(auth.sessions[slot].expires) {
			slot = index
		}
	}
	if len(auth.sessions) < maxWebSessions {
		auth.sessions = append(auth.sessions, session)
	} else {
		auth.sessions[slot] = session
	}
	return session.token, nil
}
func authCookie(w http.ResponseWriter, r *http.Request, token string, clear bool) {
	cookie := &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   requestScheme(r) == "https",
		SameSite: http.SameSiteLaxMode,
	}
	if clear {
		cookie.MaxAge = -1
	}
	http.SetCookie(w, cookie)
}
func publicWebPath(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	switch path {
	case "/api/auth/login", "/api/auth/status", "/api/auth/logout", "/api/auth/refresh-token", "/", "index.shtm", "/index.shtm", "/favicon.ico":
		return true
	}
	return path == "/web" || strings.HasPrefix(path, "/web/")
}
func (s *Server) webAuthGate(w http.ResponseWriter, r *http.Request) bool {
	if publicWebPath(r.URL.Path) || !s.authConfigured() {
		return false
	}
	token, now := requestWebToken(r), s.Now()
	s.auth.mu.Lock()
	denied := len(s.auth.users) > 0 && s.auth.sessionUserLocked(token, now) == ""
	s.auth.mu.Unlock()
	if denied {
		authFailure(401, "unauthorized", "Login required").write(w, r)
	}
	return denied
}
func boxToken(r *http.Request) ([]byte, bool) {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "BD ")
	if !ok || len(raw) != 64 {
		return nil, false
	}
	token, err := hex.DecodeString(raw)
	return token, err == nil
}

var (
	errInvalidUser     = errors.New("invalid username or password")
	errInvalidPassword = errors.New("invalid password")
	errUserExists      = errors.New("user already exists")
	errUserLimit       = errors.New("user limit reached")
	errUserNotFound    = errors.New("user not found")
)

func (auth *webAuth) changeUserLocked(operation, username, password string) ([]webUser, error) {
	index := auth.findUserLocked(username)
	users := append([]webUser{}, auth.users...)
	switch operation {
	case "create":
		if !validWebUsername(username) || len(password) < 4 {
			return nil, errInvalidUser
		}
		if index >= 0 {
			return nil, errUserExists
		}
		if len(users) >= maxWebUsers {
			return nil, errUserLimit
		}
	case "password", "delete":
		if operation == "password" && len(password) < 4 {
			return nil, errInvalidPassword
		}
		if index < 0 {
			return nil, errUserNotFound
		}
	default:
		return nil, errors.New("unsupported user operation")
	}
	if operation == "delete" {
		users = append(users[:index], users[index+1:]...)
		return users, nil
	}
	name := username
	if operation == "password" {
		name = users[index].Username
	}
	user, err := saltedUser(name, password)
	if err != nil {
		return nil, err
	}
	if operation == "create" {
		users = append(users, user)
		return users, nil
	}
	user.Role = users[index].Role
	user.AreaID = users[index].AreaID
	user.Boxes = append([]string(nil), users[index].Boxes...)
	users[index] = user
	return users, nil
}
