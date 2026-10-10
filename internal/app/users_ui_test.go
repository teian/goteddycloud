// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
)

func TestUsersAreBuiltIntoApplication(t *testing.T) {
	s := fixtureApp(t)
	reply := perform(s, http.MethodGet, "/api/plugins/get", nil, false)
	var names []string
	if err := json.Unmarshal(reply.Body.Bytes(), &names); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(names, "goteddycloud-users") {
		t.Fatal("user management advertised as a plugin")
	}
	for path, target := range map[string]string{
		"/tools/users":                           "../web/settings/users",
		"/plugins/goteddycloud-users/":           "../../web/settings/users",
		"/plugins/goteddycloud-users/index.html": "../../web/settings/users",
	} {
		reply := perform(s, http.MethodGet, path, nil, false)
		if reply.Code != http.StatusFound || reply.Header().Get("Location") != target {
			t.Fatalf("%s: %d %s", path, reply.Code, reply.Header().Get("Location"))
		}
	}
	for _, path := range []string{"/plugins/goteddycloud-users/plugin.json", "/plugins/goteddycloud-users/preview.png", "/tools/users.mjs"} {
		if reply := perform(s, http.MethodGet, path, nil, false); reply.Code != http.StatusNotFound {
			t.Fatalf("obsolete user plugin resource %s: %d", path, reply.Code)
		}
	}
}
