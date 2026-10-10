// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"goteddycloud/internal/config"
	"goteddycloud/internal/teststorage"
)

func catalogTestUsers(t *testing.T, s *Server) (accessClient, accessClient, accessClient, webArea) {
	t.Helper()
	admin := accessClient{t: t, server: s}
	admin.call("POST", "/api/auth/users/create", `{"username":"admin","password":"secret"}`, 200)
	admin.call("POST", "/api/auth/enabled", `{"enabled":true}`, 200)
	admin = admin.login("admin")
	admin.call("POST", "/api/auth/users/create", `{"username":"alice","password":"secret"}`, 200)
	admin.call("POST", "/api/auth/users/create", `{"username":"bob","password":"secret","role":"reader"}`, 200)
	area, _ := s.resolveArea("alice")
	return admin, admin.login("alice"), admin.login("bob"), area
}
func grantCatalog(t *testing.T, client accessClient, area webArea, all bool, items []catalogItem, status int) {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"area": area.ID, "username": "bob", "all": all, "items": items})
	client.call("POST", "/api/auth/catalog/grant", string(data), status)
}
func storedCatalogItem(t *testing.T, s *Server, area webArea, item catalogItem, data string) {
	t.Helper()
	target, err := s.areaServer(area)
	if err != nil {
		t.Fatal(err)
	}
	root, _, err := target.Content.ManagedRoot(target.Settings.Snapshot(""), item.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.MkdirAll(filepath.Dir(item.Path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteAtomic(item.Path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func catalogURL(route string, area webArea, item catalogItem) string {
	return "/api/auth/catalog/" + route + "?" + url.Values{"area": {area.ID}, "root": {item.Root}, "path": {item.Path}}.Encode()
}
func TestCatalogShares(t *testing.T) {
	for _, kind := range []string{"filesystem", "s3"} {
		t.Run(kind, func(t *testing.T) {
			var s *Server
			if kind == "filesystem" {
				s = fixtureApp(t)
			} else {
				base, backend := remoteFixture(t)
				settings, err := config.OpenWithStorage(base, backend, false)
				if err != nil {
					t.Fatal(err)
				}
				s, err = New(settings)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { s.Close(); settings.Close() }()
			}
			admin, alice, bob, area := catalogTestUsers(t, s)
			selected := catalogItem{"content", "12345678/ABCDEF01"}
			private := catalogItem{"library", "private.mp3"}
			later := catalogItem{"library", "nested/later.ogg"}
			storedCatalogItem(t, s, area, selected, "selected audio")
			storedCatalogItem(t, s, area, private, "private audio")
			// Raw sidecars and arbitrary custom catalog fields must never reach recipients.
			storedCatalogItem(t, s, area, catalogItem{"content", selected.Path + ".json"}, `{"tonie_model":"share-model","cloud_auth":"SECRET","source":"/private/key","cloud_ruid":"PRIVATE"}`)
			alice.call("POST", "/api/toniesCustomJsonUpsert", `{"model":"share-model","series":"Series","episodes":"Episode","pic":"https://secret.invalid/","source":"SECRET"}`, 200)
			bob.call("GET", catalogURL("items", area, catalogItem{}), "", 403)
			grantCatalog(t, alice, area, false, []catalogItem{selected}, 200)
			list := bob.call("GET", catalogURL("items", area, catalogItem{}), "", 200)
			for _, secret := range []string{"SECRET", "PRIVATE", "/private/key", "secret.invalid", "private.mp3"} {
				if strings.Contains(list.Body.String(), secret) {
					t.Fatalf("leaked %q: %s", secret, list.Body.String())
				}
			}
			if !strings.Contains(list.Body.String(), "Series Episode") {
				t.Fatal("title missing", list.Body.String())
			}
			if got := bob.call("GET", catalogURL("download", area, selected), "", 200).Body.String(); got != "selected audio" {
				t.Fatal(got)
			}
			bob.call("GET", catalogURL("download", area, private), "", 403)
			bob.call("GET", "/content/json/get/12345678ABCDEF01?area="+area.ID, "", 403)
			bob.call("GET", "/api/fileIndex?area="+area.ID, "", 403)
			bob.call("POST", "/api/auth/area", `{"area":"`+area.ID+`"}`, 403)
			grantCatalog(t, bob, area, true, nil, 403)
			// Range requests work without allowing metadata/source resolution.
			req := httptest.NewRequest("GET", catalogURL("download", area, selected), nil)
			req.Header.Set("Authorization", "Bearer "+bob.token)
			req.Header.Set("Range", "bytes=0-7")
			reply := httptest.NewRecorder()
			s.Management(reply, req)
			if reply.Code != 206 || reply.Body.String() != "selected" {
				t.Fatal(reply.Code, reply.Body.String())
			}
			// A restart restores grants; a password edit preserves recipient identity.
			restored, err := New(s.Settings)
			if err != nil {
				t.Fatal(err)
			}
			restoredBob := accessClient{t: t, server: restored}.login("bob")
			restoredBob.call("GET", catalogURL("download", area, selected), "", 200)
			restored.Close()
			bob.call("POST", "/api/auth/password", `{"currentPassword":"secret","password":"changed"}`, 200)
			login := admin.call("POST", "/api/auth/login", `{"username":"bob","password":"changed"}`, 200)
			var token struct{ Token string }
			json.Unmarshal(login.Body.Bytes(), &token)
			bob.token = token.Token
			bob.call("GET", catalogURL("download", area, selected), "", 200)
			// Full grants include new files, selected grants do not.
			storedCatalogItem(t, s, area, later, "later audio")
			bob.call("GET", catalogURL("download", area, later), "", 403)
			grantCatalog(t, alice, area, true, nil, 200)
			allFiles := bob.call("GET", catalogURL("items", area, catalogItem{}), "", 200).Body.String()
			if !strings.Contains(allFiles, "private.mp3") || !strings.Contains(allFiles, "later.ogg") || strings.Contains(allFiles, ".json") || strings.Contains(allFiles, "SECRET") {
				t.Fatal("full catalog projection", allFiles)
			}
			grantCatalog(t, alice, area, false, []catalogItem{{"library", "missing.mp3"}}, 400)
			bob.call("GET", catalogURL("download", area, later), "", 200)
			bob.call("GET", catalogURL("download", area, catalogItem{"content", selected.Path + ".json"}), "", 400)
			for _, item := range []catalogItem{{"library", "../config/web_users.json"}, {"library", "/private.mp3"}, {"custom_img", "image.mp3"}, {"library", "nested/../private.mp3"}, {"library", "key.pem"}, {"library", ".hidden.mp3"}} {
				bob.call("GET", catalogURL("download", area, item), "", 400)
				grantCatalog(t, alice, area, false, []catalogItem{item}, 400)
			}
			// Replacement reduces access immediately, then revocation removes the catalog.
			grantCatalog(t, alice, area, false, []catalogItem{selected}, 200)
			bob.call("GET", catalogURL("download", area, later), "", 403)
			alice.call("POST", "/api/auth/catalog/revoke", `{"area":"`+area.ID+`","username":"bob"}`, 200)
			bob.call("GET", catalogURL("items", area, catalogItem{}), "", 403)
			grantCatalog(t, alice, area, true, nil, 200)
			admin.call("POST", "/api/auth/users/delete", `{"username":"bob"}`, 200)
			admin.call("POST", "/api/auth/users/create", `{"username":"bob","password":"secret"}`, 200)
			bob = admin.login("bob")
			bob.call("GET", catalogURL("items", area, catalogItem{}), "", 403)
			current, _ := s.areaByID(area.ID)
			if len(current.Shares) != 0 {
				t.Fatal("deleted account grant retained")
			}
			// Read-only/private and shared-area editors cannot reshare.
			admin.call("POST", "/api/auth/users/access", `{"username":"alice","role":"reader"}`, 200)
			grantCatalog(t, alice, area, true, nil, 403)
			shared := createSharedArea(t, admin, "Shared", []areaMember{{"alice", roleEditor}, {"bob", roleReader}})
			grantCatalog(t, alice, shared, true, nil, 403)
			shared.Members[0].Role = roleAdmin
			updateSharedArea(t, admin, shared, 200)
			grantCatalog(t, alice, shared, true, nil, 200)
			alice.call("GET", "/tools/shares", "", 200)
			bob.call("GET", "/tools/shares.mjs", "", 200)
		})
	}
}

func TestCatalogShareSymlinks(t *testing.T) {
	s := fixtureApp(t)
	_, alice, bob, area := catalogTestUsers(t, s)
	storedCatalogItem(t, s, area, catalogItem{"library", "private.json"}, "SECRET")
	target, err := s.areaServer(area)
	if err != nil {
		t.Fatal(err)
	}
	library := target.Settings.String("", "internal.librarydirfull")
	if err := os.Symlink("private.json", filepath.Join(library, "alias.mp3")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".", filepath.Join(library, "loop")); err != nil {
		t.Fatal(err)
	}
	grantCatalog(t, alice, area, false, []catalogItem{{"library", "alias.mp3"}}, 400)
	grantCatalog(t, alice, area, true, nil, 200)
	bob.call("GET", catalogURL("download", area, catalogItem{"library", "alias.mp3"}), "", 404)
	reply := bob.call("GET", catalogURL("items", area, catalogItem{}), "", 200)
	if strings.Contains(reply.Body.String(), "alias") || strings.Contains(reply.Body.String(), "SECRET") {
		t.Fatal(reply.Body.String())
	}
}

func TestCatalogShareSaveFailure(t *testing.T) {
	base := t.TempDir()
	var fail atomic.Bool
	backend := teststorage.New(t, base, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if fail.Load() && r.Method == "PUT" && strings.HasSuffix(r.URL.Path, "/config/web_users.json") {
				http.Error(w, "injected failure", http.StatusForbidden)
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
	s, err := New(settings)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, alice, bob, area := catalogTestUsers(t, s)
	fail.Store(true)
	grantCatalog(t, alice, area, true, nil, 500)
	bob.call("GET", catalogURL("items", area, catalogItem{}), "", 403)
	fail.Store(false)
	grantCatalog(t, alice, area, true, nil, 200)
	fail.Store(true)
	alice.call("POST", "/api/auth/catalog/revoke", `{"area":"`+area.ID+`","username":"bob"}`, 500)
	bob.call("GET", catalogURL("items", area, catalogItem{}), "", 200)
	restored, err := New(settings)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	accessClient{t: t, server: restored}.login("bob").call("GET", catalogURL("items", area, catalogItem{}), "", 200)
}

func TestCatalogGrantValidation(t *testing.T) {
	users := []webUser{{Username: "bob", AreaID: "recipient"}}
	item := catalogItem{"library", "story.mp3"}
	cases := map[string][]catalogGrant{
		"missing recipient":   {{Recipient: "deleted", All: true}},
		"empty selection":     {{Recipient: "recipient"}},
		"ambiguous scope":     {{Recipient: "recipient", All: true, Items: []catalogItem{item}}},
		"duplicate file":      {{Recipient: "recipient", Items: []catalogItem{item, item}}},
		"duplicate recipient": {{Recipient: "recipient", All: true}, {Recipient: "recipient", All: true}},
		"sidecar":             {{Recipient: "recipient", Items: []catalogItem{{"library", "story.mp3.json"}}}},
	}
	for name, grants := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateCatalogGrants(users, grants); err == nil {
				t.Fatal("invalid grant accepted")
			}
		})
	}
}
