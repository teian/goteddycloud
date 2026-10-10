// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDevelopWebAuthLifecycle(t *testing.T) {
	s := fixtureApp(t)
	request := func(method, path, payload, token string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(payload))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		s.Management(w, r)
		return w
	}
	check := func(w *httptest.ResponseRecorder, status int) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
	}
	check(request("POST", "/api/auth/enabled", `{"enabled":true}`, ""), 400)
	check(request("POST", "/api/auth/login", "username=admin&passwordHash=x", ""), 400)
	check(request("POST", "/api/auth/users/create", `{"username":"Admin","password":"secret"}`, ""), 200)
	check(request("POST", "/api/auth/users/create", `{"username":"admin","password":"secret"}`, ""), 409)
	check(request("POST", "/api/auth/enabled", `{"enabled":true}`, ""), 200)
	for _, p := range []string{"/api/settings/getIndex", "/v1/time", "/robots.txt", "/api/auth/users/get", "/api/unknown"} {
		check(request("GET", p, "", ""), 401)
	}
	check(request("GET", "/api/auth/status", "", ""), 200)
	check(request("GET", "/", "", ""), 301)
	w := request("POST", "/api/auth/login", `{"username":"admin","password":"secret"}`, "")
	check(w, 200)
	var login struct {
		Token    string `json:"token"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &login); err != nil || len(login.Token) != 64 || login.Username != "Admin" {
		t.Fatal(w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookie || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].Secure {
		t.Fatal(cookies)
	}
	check(request("GET", "/api/settings/getIndex", "", login.Token), 200)
	r := httptest.NewRequest("GET", "/api/auth/users/get", nil)
	r.AddCookie(cookies[0])
	w = httptest.NewRecorder()
	s.Management(w, r)
	check(w, 200)
	check(request("POST", "/api/auth/refresh-token", "", login.Token), 200)
	file := filepath.Join(s.Settings.Snapshot("").String("internal.configdirfull"), "web_users.json")
	b, err := os.ReadFile(file)
	if err != nil || strings.Contains(string(b), "secret") {
		t.Fatal(err, string(b))
	}
	fresh := &Server{Settings: s.Settings}
	if err := fresh.loadWebUsers(); err != nil || len(fresh.auth.users) != 1 || fresh.auth.users[0].Username != "Admin" {
		t.Fatal("persisted user reload", err)
	}
	info, _ := os.Stat(file)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	check(request("POST", "/api/auth/users/updatePassword", `{"username":"Admin","password":"changed"}`, login.Token), 200)
	check(request("GET", "/api/auth/logout", "", login.Token), 200)
	check(request("GET", "/api/settings/getIndex", "", login.Token), 401)
	check(request("POST", "/api/auth/login", `{"username":"Admin","password":"secret"}`, ""), 401)
	w = request("POST", "/api/auth/login", `{"username":"Admin","password":"changed"}`, "")
	check(w, 200)
	_ = json.Unmarshal(w.Body.Bytes(), &login)
	check(request("POST", "/api/auth/users/delete", `{"username":"Admin"}`, login.Token), 200)
	check(request("GET", "/api/settings/getIndex", "", ""), 200)
	if s.Settings.Snapshot("").Bool("frontend.web_auth_enabled") {
		t.Fatal("last user did not disable protection")
	}
}
func TestWebAuthLimitsExpiryAndOverride(t *testing.T) {
	s := fixtureApp(t)
	now := time.Now()
	s.Now = func() time.Time { return now }
	for i := 0; i < 6; i++ {
		w := perform(s, "POST", "/api/auth/login", []byte(`{"username":"missing","password":"bad"}`), false)
		want := 401
		if i == 5 {
			want = 429
		}
		if w.Code != want {
			t.Fatal(i, w.Code, w.Body.String())
		}
	}
	now = now.Add(601 * time.Second)
	if w := perform(s, "POST", "/api/auth/login", []byte(`{"username":"missing","password":"bad"}`), false); w.Code != 401 {
		t.Fatal(w.Code)
	}
	_ = perform(s, "POST", "/api/auth/users/create", []byte(`{"username":"admin","password":"secret"}`), false)
	_ = s.Settings.Set("", "frontend.web_auth_enabled", "true")
	t.Setenv("TEDDYCLOUD_WEB_AUTH_DISABLE", "YES")
	if w := perform(s, "GET", "/api/settings/getIndex", nil, false); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := perform(s, "POST", "/api/auth/enabled", []byte(`{"enabled":true}`), false); w.Code != 400 {
		t.Fatal(w.Code)
	}
	s.auth.mu.Lock()
	token, err := s.auth.newSessionLocked("admin", s.Now())
	s.auth.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(sessionTTL + time.Second)
	r := httptest.NewRequest("POST", "/api/auth/refresh-token", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	s.Management(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
}
func TestManagementPreflightAndSecrets(t *testing.T) {
	s := fixtureApp(t)
	_ = s.Settings.Set("", "core.allowOrigin", "https://frontend.invalid")
	w := perform(s, "OPTIONS", "/api/unknown", nil, false)
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Headers") != "Content-Type, Authorization" || w.Header().Get("Vary") != "Origin" {
		t.Fatal(w)
	}
	w = perform(s, "GET", "/api/settings/getIndex?internal=true&nolevel=true", nil, false)
	var index struct {
		Options []struct {
			Level int `json:"level"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &index); err != nil {
		t.Fatal(err)
	}
	for _, d := range index.Options {
		if d.Level == 99 {
			t.Fatal("secret included")
		}
	}
}
func TestBoxToken(t *testing.T) {
	for _, v := range []struct {
		raw  string
		want bool
	}{{"BD " + strings.Repeat("00", 32), true}, {"Digest abc", false}, {"BD " + strings.Repeat("zz", 32), false}, {"BD 00", false}} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", v.raw)
		_, ok := boxToken(r)
		if ok != v.want {
			t.Fatal(v)
		}
	}
}
