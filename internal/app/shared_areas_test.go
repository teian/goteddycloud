// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"goteddycloud/internal/config"
	"goteddycloud/internal/teststorage"
)

func createSharedArea(t *testing.T, admin accessClient, name string, members []areaMember) webArea {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"name": name, "members": members})
	reply := admin.call("POST", "/api/auth/areas/create", string(data), 200)
	var payload struct {
		Area webArea `json:"area"`
	}
	if err := json.Unmarshal(reply.Body.Bytes(), &payload); err != nil || payload.Area.ID == "" {
		t.Fatal("create area", reply.Body.String(), err)
	}
	return payload.Area
}
func updateSharedArea(t *testing.T, client accessClient, area webArea, status int) {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"area": area.ID, "name": area.Name, "members": area.Members})
	client.call("POST", "/api/auth/areas/update", string(data), status)
}
func TestSharedAreasDifferentMemberRoles(t *testing.T) {
	for _, kind := range []string{"filesystem", "s3"} {
		t.Run(kind, func(t *testing.T) {
			var server *Server
			if kind == "filesystem" {
				server = fixtureApp(t)
			} else {
				base, backend := remoteFixture(t)
				settings, err := config.OpenWithStorage(base, backend, false)
				if err != nil {
					t.Fatal(err)
				}
				server, err = New(settings)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { server.Close(); settings.Close() }()
			}
			admin := accessClient{t: t, server: server}
			admin.call("POST", "/api/auth/users/create", `{"username":"admin","password":"secret"}`, 200)
			admin.call("POST", "/api/auth/enabled", `{"enabled":true}`, 200)
			admin = admin.login("admin")
			for _, body := range []string{`{"username":"alice","password":"secret"}`, `{"username":"bob","password":"secret","role":"reader"}`, `{"username":"cara","password":"secret"}`} {
				admin.call("POST", "/api/auth/users/create", body, 200)
			}
			alice, bob, cara := admin.login("alice"), admin.login("bob"), admin.login("cara")
			family := createSharedArea(t, admin, "Family", []areaMember{{"alice", roleAdmin}, {"bob", roleEditor}, {"cara", roleReader}})
			school := createSharedArea(t, admin, "School", []areaMember{{"alice", roleReader}, {"cara", roleEditor}})
			// Membership is independent of the account's default role.
			bob.call("POST", "/content/json/set/0000000100000002", "tonie_model=private", 403)
			bob.call("POST", "/content/json/set/0000000100000002?area="+family.ID, "tonie_model=family-content", 200)
			alice.call("GET", "/content/json/get/0000000100000002?area="+family.ID, "", 200)
			if reply := cara.call("GET", "/content/json/get/0000000100000002?area="+family.ID, "", 200); !strings.Contains(reply.Body.String(), "family-content") {
				t.Fatal("members do not share content")
			}
			cara.call("POST", "/content/json/set/0000000100000002?area="+family.ID, "tonie_model=forbidden", 403)
			cara.call("POST", "/content/json/set/0000000100000002?area="+school.ID, "tonie_model=school-content", 200)
			alice.call("POST", "/content/json/set/0000000100000002?area="+school.ID, "tonie_model=forbidden", 403)
			bob.call("GET", "/content/json/get/0000000100000002?area="+school.ID, "", 403)
			bob.call("POST", "/api/auth/area", `{"area":"`+school.ID+`"}`, 403)
			bob.call("POST", "/api/auth/area", `{"area":"`+family.ID+`"}`, 200)
			bob.call("POST", "/content/json/set/0000000100000002", "tonie_model=shared-via-session", 200)
			if reply := bob.call("GET", "/api/auth/areas", "", 200); strings.Contains(reply.Body.String(), school.ID) {
				t.Fatal("foreign area listed")
			}
			// Editor and reader cannot administer members; area admin cannot escalate globally.
			updateSharedArea(t, bob, family, 403)
			updateSharedArea(t, cara, family, 403)
			alice.call("POST", "/api/auth/users/access", `{"username":"alice","role":"admin"}`, 403)
			alice.call("POST", "/api/auth/areas/create", `{"name":"forbidden"}`, 403)
			alice.call("POST", "/api/auth/areas/delete", `{"area":"`+family.ID+`"}`, 403)
			alice.call("POST", "/api/auth/areas/update", `{"area":"`+family.ID+`","boxes":["001122334455"]}`, 403)
			// Area admin can change another member's role; existing tokens see it immediately.
			family.Members[1].Role = roleReader
			updateSharedArea(t, alice, family, 200)
			bob.call("POST", "/content/json/set/0000000100000002", "tonie_model=forbidden", 403)
			// A shared box follows its area, independently of any single member.
			admin.call("POST", "/api/auth/areas/update", `{"area":"`+family.ID+`","boxes":["001122334455"]}`, 200)
			admin.call("POST", "/api/auth/areas/update", `{"area":"`+school.ID+`","boxes":["001122334455"]}`, 409)
			alice.call("POST", "/api/settings/set/toniebox.max_vol_spk?area="+family.ID+"&overlay=001122334455", "2", 200)
			bob.call("POST", "/api/settings/set/toniebox.max_vol_spk?overlay=001122334455", "1", 403)
			cara.call("GET", "/api/getBoxes?area="+school.ID+"&overlay=001122334455", "", 403)
			target, err := server.boxServer("001122334455")
			if err != nil || target.areaID != family.ID || target.Settings.String("001122334455", "toniebox.max_vol_spk") != "2" {
				t.Fatal("shared device routing", err)
			}
			// Failed membership changes retain all previous rights and persisted state.
			duplicate := cloneArea(family)
			duplicate.Members = append(duplicate.Members, duplicate.Members[0])
			updateSharedArea(t, alice, duplicate, 409)
			missing := cloneArea(family)
			missing.Members = append(missing.Members, areaMember{"missing", roleEditor})
			updateSharedArea(t, alice, missing, 409)
			invalid := cloneArea(family)
			invalid.Members[0].Role = "superuser"
			updateSharedArea(t, alice, invalid, 409)
			// Password/account-role updates must not overwrite independently configured memberships.
			admin.call("POST", "/api/auth/users/access", `{"username":"bob","role":"editor"}`, 200)
			bob.call("POST", "/content/json/set/0000000100000002", "tonie_model=still-forbidden", 403)
			bob.call("POST", "/api/auth/password", `{"currentPassword":"secret","password":"newpassword"}`, 200)
			record, _ := server.areaByID(family.ID)
			if record.role("bob") != roleReader {
				t.Fatal("password changed membership")
			}
			// Removing the member's original owner does not orphan shared data for others.
			admin.call("POST", "/api/auth/users/delete", `{"username":"alice"}`, 200)
			alice.call("GET", "/api/auth/areas", "", 401)
			cara.call("GET", "/content/json/get/0000000100000002?area="+family.ID, "", 200)
			target, err = server.boxServer("001122334455")
			if err != nil || target.areaID != family.ID {
				t.Fatal("deleted member broke device routing", err)
			}
			admin.call("POST", "/api/auth/users/create", `{"username":"alice","password":"secret"}`, 200)
			recreated := admin.login("alice")
			recreated.call("GET", "/api/fileIndex?area="+family.ID, "", 403)
			// Membership removal invalidates explicit and session area access.
			family.Members = []areaMember{{"cara", roleReader}}
			updateSharedArea(t, admin, family, 200)
			response := bob.call("POST", "/api/auth/login", `{"username":"bob","password":"newpassword"}`, 200)
			var login struct{ Token string }
			json.Unmarshal(response.Body.Bytes(), &login)
			bob.token = login.Token
			bob.call("GET", "/api/fileIndex?area="+family.ID, "", 403)
			// Reconstruct the entire server using stored JSON, then reuse shared data/roles.
			reload, err := New(server.Settings)
			if err != nil {
				t.Fatal(err)
			}
			defer reload.Close()
			persisted, ok := reload.areaByID(family.ID)
			if !ok || persisted.role("cara") != roleReader || persisted.role("bob") != "" {
				t.Fatal("membership persistence", persisted)
			}
			reloadedCara := accessClient{t: t, server: reload}.login("cara")
			if reply := reloadedCara.call("GET", "/content/json/get/0000000100000002?area="+family.ID, "", 200); !strings.Contains(reply.Body.String(), "shared-via-session") {
				t.Fatal("shared files lost on reconstruction")
			}
			// Delete only independent areas; default areas remain stable account references.
			admin.call("POST", "/api/auth/areas/delete", `{"area":"cara"}`, 409)
			admin.call("POST", "/api/auth/areas/delete", `{"area":"`+school.ID+`"}`, 200)
			cara.call("GET", "/api/fileIndex?area="+school.ID, "", 403)
		})
	}
}

func TestSharedMembershipRevocationClosesSSE(t *testing.T) {
	server := fixtureApp(t)
	admin := accessClient{t: t, server: server}
	admin.call("POST", "/api/auth/users/create", `{"username":"admin","password":"secret"}`, 200)
	admin.call("POST", "/api/auth/enabled", `{"enabled":true}`, 200)
	admin = admin.login("admin")
	admin.call("POST", "/api/auth/users/create", `{"username":"member","password":"secret"}`, 200)
	member := admin.login("member")
	area := createSharedArea(t, admin, "Team", []areaMember{{"member", roleReader}})
	endpoint := httptest.NewServer(http.HandlerFunc(server.Management))
	defer endpoint.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "GET", endpoint.URL+"/api/sse?area="+area.ID, nil)
	request.Header.Set("Authorization", "Bearer "+member.token)
	reply, err := endpoint.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer reply.Body.Close()
	if reply.StatusCode != 200 {
		t.Fatal(reply.StatusCode)
	}
	done := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, reply.Body); done <- err }()
	area.Members = []areaMember{}
	updateSharedArea(t, admin, area, 200)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("revoked shared stream stayed open")
	}
	member.call("GET", "/api/sse?area="+area.ID, "", 403)
}

func TestPrivateAreaRegistryMigrationKeepsStorage(t *testing.T) {
	server := fixtureApp(t)
	user, err := saltedUser("legacy", "secret")
	if err != nil {
		t.Fatal(err)
	}
	user.Boxes = []string{"001122334455"}
	if err := server.saveWebUsers([]webUser{user}); err != nil {
		t.Fatal(err)
	}
	areaServer, err := server.userArea(user)
	if err != nil {
		t.Fatal(err)
	}
	root, _, err := areaServer.Content.ManagedRoot(areaServer.Settings.Snapshot(""), "library")
	if err != nil {
		t.Fatal(err)
	}
	if err := root.WriteAtomic("preserved.txt", []byte("legacy data"), 0600); err != nil {
		t.Fatal(err)
	}
	rootPath := areaServer.Settings.Snapshot("").Base
	// Simulate the prior private-area format, before an independent area registry.
	data, _ := json.Marshal(map[string]any{"users": []webUser{user}})
	configRoot, err := server.Settings.Storage().OpenRoot(server.Settings.String("", "internal.configdirfull"))
	if err != nil {
		t.Fatal(err)
	}
	if err := configRoot.WriteAtomic("web_users.json", data, 0600); err != nil {
		t.Fatal(err)
	}
	configRoot.Close()
	reloaded, err := New(server.Settings)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	record, ok := reloaded.areaByID(user.AreaID)
	if !ok || record.role("legacy") != roleAdmin || len(record.Boxes) != 1 {
		t.Fatal("private area migration", record)
	}
	child, err := reloaded.areaServer(record)
	if err != nil || child.Settings.Snapshot("").Base != rootPath {
		t.Fatal("private area storage moved", err)
	}
	library, _, err := child.Content.ManagedRoot(child.Settings.Snapshot(""), "library")
	if err != nil {
		t.Fatal(err)
	}
	if data, err := library.ReadFile("preserved.txt"); err != nil || string(data) != "legacy data" {
		t.Fatal("legacy files lost", err)
	}
	// Corrupt membership data must fail startup rather than grant accidental access.
	for name, mutate := range map[string]func(*webArea){
		"missing user":     func(area *webArea) { area.Members = append(area.Members, areaMember{"missing", roleAdmin}) },
		"duplicate member": func(area *webArea) { area.Members = append(area.Members, area.Members[0]) },
		"invalid role":     func(area *webArea) { area.Members[0].Role = "root" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := cloneArea(record)
			mutate(&invalid)
			data, _ := json.Marshal(webUsersFile{Users: []webUser{user}, Areas: []webArea{invalid}})
			root, err := server.Settings.Storage().OpenRoot(server.Settings.String("", "internal.configdirfull"))
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if err := root.WriteAtomic("web_users.json", data, 0600); err != nil {
				t.Fatal(err)
			}
			if broken, err := New(server.Settings); err == nil {
				broken.Close()
				t.Fatal("invalid membership database accepted")
			}
		})
	}
}

func TestSharedAreaSaveFailureRetainsMemberships(t *testing.T) {
	base := t.TempDir()
	var fail atomic.Bool
	backend := teststorage.New(t, base, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if fail.Load() && r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/config/web_users.json") {
				http.Error(w, "injected publication failure", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	settings, err := config.OpenWithStorage(base, backend, false)
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	server, err := New(settings)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	admin := accessClient{t: t, server: server}
	admin.call("POST", "/api/auth/users/create", `{"username":"admin","password":"secret"}`, 200)
	admin.call("POST", "/api/auth/enabled", `{"enabled":true}`, 200)
	admin = admin.login("admin")
	admin.call("POST", "/api/auth/users/create", `{"username":"member","password":"secret"}`, 200)
	member := admin.login("member")
	area := createSharedArea(t, admin, "Team", []areaMember{{"member", roleEditor}})
	fail.Store(true)
	area.Members[0].Role = roleReader
	updateSharedArea(t, admin, area, 500)
	live, ok := server.areaByID(area.ID)
	if !ok || live.role("member") != roleEditor {
		t.Fatal("failed write changed live membership")
	}
	member.call("GET", "/api/auth/areas", "", 200)
	fail.Store(false)
	restored, err := New(settings)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	stored, ok := restored.areaByID(area.ID)
	if !ok || stored.role("member") != roleEditor {
		t.Fatal("failed write changed persisted membership")
	}
	updateSharedArea(t, admin, area, 200)
}

func TestShareExistingDefaultAreaAndDeleteOriginalAccount(t *testing.T) {
	server := fixtureApp(t)
	admin := accessClient{t: t, server: server}
	admin.call("POST", "/api/auth/users/create", `{"username":"admin","password":"secret"}`, 200)
	admin.call("POST", "/api/auth/enabled", `{"enabled":true}`, 200)
	admin = admin.login("admin")
	for _, name := range []string{"alice", "bob"} {
		admin.call("POST", "/api/auth/users/create", `{"username":"`+name+`","password":"secret"}`, 200)
	}
	alice, bob := admin.login("alice"), admin.login("bob")
	alice.call("POST", "/content/json/set/0000000100000002", "tonie_model=existing-private", 200)
	original, _ := server.resolveArea("alice")
	admin.call("POST", "/api/auth/areas/update", `{"area":"alice","members":[{"username":"alice","role":"editor"},{"username":"bob","role":"reader"}]}`, 200)
	bob.call("POST", "/api/auth/area", `{"area":"alice"}`, 200)
	if reply := bob.call("GET", "/api/auth/status", "", 200); !strings.Contains(reply.Body.String(), `"areaRole":"reader"`) {
		t.Fatal("active role missing", reply.Body.String())
	}
	bob.call("GET", "/content/json/get/0000000100000002", "", 200)
	bob.call("POST", "/content/json/set/0000000100000002", "tonie_model=forbidden", 403)
	admin.call("POST", "/api/auth/users/delete", `{"username":"alice"}`, 200)
	bob.call("GET", "/content/json/get/0000000100000002", "", 200)
	admin.call("POST", "/api/auth/users/create", `{"username":"alice","password":"secret"}`, 200)
	newAlice := admin.login("alice")
	newAlice.call("GET", "/content/json/get/0000000100000002?area="+original.ID, "", 403)
	// Stored selections use IDs, so reusing the old username cannot retarget Bob's session.
	if reply := bob.call("GET", "/content/json/get/0000000100000002", "", 200); !strings.Contains(reply.Body.String(), "existing-private") {
		t.Fatal("session selector changed to recreated account")
	}
	bob.call("GET", "/api/fileIndex?area=alice", "", 403)
}
