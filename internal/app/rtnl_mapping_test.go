// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRTNLMappingManagementCacheAndAccess(t *testing.T) {
	s := fixtureApp(t)
	const route = "/api/rtnl/mapping"
	first := perform(s, "GET", route, nil, false)
	if first.Code != 200 || !strings.HasPrefix(first.Header().Get("Content-Type"), "application/json") || !strings.Contains(first.Body.String(), `"6-8498"`) {
		t.Fatal(first.Code, first.Body.String())
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing registry revision")
	}
	r := httptest.NewRequest("GET", route, nil)
	r.Header.Set("If-None-Match", etag)
	w := httptest.NewRecorder()
	s.Management(w, r)
	if w.Code != 304 || w.Body.Len() != 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	if reply := perform(s, "GET", route, nil, true); reply.Code != 404 {
		t.Fatal("mapping exposed on box API", reply.Code)
	}
	if reply := perform(s, "POST", route, nil, false); reply.Code != 405 {
		t.Fatal(reply.Code)
	}
	admin := accessClient{t: t, server: s}
	admin.call("POST", "/api/auth/users/create", `{"username":"admin","password":"secret"}`, 200)
	admin.call("POST", "/api/auth/enabled", `{"enabled":true}`, 200)
	admin = admin.login("admin")
	admin.call("POST", "/api/auth/users/create", `{"username":"reader","password":"secret","role":"reader"}`, 200)
	reader := admin.login("reader")
	reader.call("GET", route, "", 200)
	reader.call("POST", route, "", 403)
	w = httptest.NewRecorder()
	s.Management(w, r)
	if w.Code != 401 {
		t.Fatal("conditional response bypassed login", w.Code)
	}
}
