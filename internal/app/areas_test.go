// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"goteddycloud/internal/config"
	"goteddycloud/internal/storage"
)

type accessClient struct {
	t      *testing.T
	server *Server
	token  string
}

func (c accessClient) call(method, path, payload string, status int) *httptest.ResponseRecorder {
	c.t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(payload))
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	reply := httptest.NewRecorder()
	c.server.Management(reply, request)
	if reply.Code != status {
		c.t.Fatalf("%s %s: got %d, want %d: %s", method, path, reply.Code, status, reply.Body.String())
	}
	return reply
}
func (c accessClient) login(name string) accessClient {
	c.t.Helper()
	reply := c.call("POST", "/api/auth/login", `{"username":"`+name+`","password":"secret"}`, 200)
	var payload struct{ Token string }
	if err := json.Unmarshal(reply.Body.Bytes(), &payload); err != nil {
		c.t.Fatal(err)
	}
	c.token = payload.Token
	return c
}
func TestPrivateAreasAndRoles(t *testing.T) {
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
			admin.call("POST", "/api/auth/users/create", `{"username":"alice","password":"secret"}`, 200)
			admin.call("POST", "/api/auth/users/create", `{"username":"bob","password":"secret","role":"reader"}`, 200)
			alice := admin.login("alice")
			bob := admin.login("bob")
			for _, client := range []accessClient{alice, bob} {
				for _, path := range []string{"/api/auth/users/get", "/api/auth/users/details", "/api/settings/get/core.client_cert.data.key", "/api/triggerRestart", "/api/toniesJsonUpdate", "/api/cc3200/altPort?port=1234", "/api/cacheStats", "/api/fileIndex?area=admin", "/api/getBoxes?overlay=112233445566", "/api/fileIndex?area=alice&area=bob", "/api/getBoxesSuffix"} {
					client.call("GET", path, "", 403)
				}
				client.call("POST", "/api/auth/users/access", `{"username":"admin","role":"reader"}`, 403)
				client.call("POST", "/api/auth/enabled", `{"enabled":false}`, 403)
				client.call("GET", "/api/settings/getIndex", "", 200)
				client.call("GET", "/web/settings/users", "", 200)
				client.call("GET", "/api/auth/profile", "", 200)
			}
			bob.call("POST", "/api/dirCreate", "name=forbidden", 403)
			bob.call("POST", "/content/json/set/0000000100000001", "nocloud=true", 403)
			aliceUser, _ := server.userByName("alice")
			bobUser, _ := server.userByName("bob")
			aliceArea, err := server.userArea(aliceUser)
			if err != nil {
				t.Fatal(err)
			}
			bobArea, err := server.userArea(bobUser)
			if err != nil {
				t.Fatal(err)
			}
			if aliceArea.Settings.Snapshot("").Base == bobArea.Settings.Snapshot("").Base {
				t.Fatal("shared area")
			}
			root, _, err := aliceArea.Content.ManagedRoot(aliceArea.Settings.Snapshot(""), "library")
			if err != nil {
				t.Fatal(err)
			}
			if err := root.WriteAtomic("alice.txt", []byte("private to alice"), 0600); err != nil {
				t.Fatal(err)
			}
			alice.call("GET", "/api/fileIndex?special=library", "", 200)
			if reply := bob.call("GET", "/api/fileIndex?special=library", "", 200); strings.Contains(reply.Body.String(), "alice.txt") {
				t.Fatal("cross-area index")
			}
			if _, err := bobArea.Settings.Storage().ReadFile(filepath.Join(root.Name(), "alice.txt")); err == nil {
				t.Fatal("cross-area read")
			}
			admin.call("GET", "/api/fileIndex?special=library&area=alice", "", 200)
			admin.call("POST", "/api/auth/area", `{"area":"alice"}`, 200)
			if reply := admin.call("GET", "/api/fileIndex?special=library", "", 200); !strings.Contains(reply.Body.String(), "alice.txt") {
				t.Fatal("session area not selected", reply.Body.String())
			}
			admin.call("POST", "/api/auth/area", `{"area":""}`, 200)
			admin.call("POST", "/api/auth/users/access", `{"username":"admin","role":"reader"}`, 409)
			admin.call("POST", "/api/auth/users/delete", `{"username":"admin"}`, 409)
			admin.call("POST", "/api/auth/enabled", `{"enabled":false}`, 409)
			// Registered boxes are exclusively assigned and drive MQTT/device settings routing.
			const box = "112233445566"
			if err := server.Settings.Set(box, "boxName", "Alice's box"); err != nil {
				t.Fatal(err)
			}
			admin.call("POST", "/api/auth/users/access", `{"username":"alice","boxes":["112233445566"]}`, 200)
			admin.call("POST", "/api/auth/users/access", `{"username":"bob","boxes":["112233445566"]}`, 409)
			if reply := alice.call("GET", "/api/getBoxes", "", 200); !strings.Contains(reply.Body.String(), box) {
				t.Fatal("assigned box missing")
			}
			if reply := bob.call("GET", "/api/getBoxes", "", 200); strings.Contains(reply.Body.String(), box) {
				t.Fatal("foreign box exposed")
			}
			alice.call("POST", "/api/settings/set/toniebox.max_vol_spk?overlay="+box, "2", 200)
			target, err := server.boxServer(box)
			if err != nil || target != aliceArea || target.Settings.String(box, "toniebox.max_vol_spk") != "2" {
				t.Fatal("owned box routing", err)
			}
			if err := server.executeBoxCommand(box, "CmdVolLimitSpk", "1"); err != nil {
				t.Fatal(err)
			}
			if aliceArea.Settings.String(box, "toniebox.max_vol_spk") != "1" {
				t.Fatal("MQTT command not routed")
			}
			bob.call("GET", "/api/settings/get/toniebox.max_vol_spk?overlay="+box, "", 403)
			// Revocation takes effect on existing sessions, without relogin.
			admin.call("POST", "/api/auth/users/access", `{"username":"alice","role":"reader","boxes":[]}`, 200)
			alice.call("POST", "/api/fileDelete", "name=alice.txt", 403)
			alice.call("GET", "/api/getBoxes?overlay="+box, "", 403)
			// Password changes revoke tokens but preserve area identity and files.
			alice.call("POST", "/api/auth/password", `{"currentPassword":"bad","password":"newpassword"}`, 403)
			alice.call("POST", "/api/auth/password", `{"currentPassword":"secret","password":"newpassword"}`, 200)
			alice.call("GET", "/api/fileIndex", "", 401)
			updated, _ := server.userByName("alice")
			if updated.AreaID != aliceUser.AreaID || updated.Role != roleReader {
				t.Fatal("password changed access/area")
			}
			fresh := &Server{Settings: server.Settings}
			if err := fresh.loadWebUsers(); err != nil {
				t.Fatal(err)
			}
			persisted, _ := fresh.userByName("alice")
			if persisted.AreaID != aliceUser.AreaID || persisted.Role != roleReader {
				t.Fatal("access not persisted")
			}
			admin.call("POST", "/api/auth/users/delete", `{"username":"bob"}`, 200)
			bob.call("GET", "/api/fileIndex", "", 401)
			admin.call("POST", "/api/auth/users/create", `{"username":"bob","password":"secret"}`, 200)
			recreated, _ := server.userByName("bob")
			if recreated.AreaID == bobUser.AreaID {
				t.Fatal("recreated account inherits private files")
			}
		})
	}
}

func TestLegacyRolesAndAreaIdentity(t *testing.T) {
	server := fixtureApp(t)
	user, err := saltedUser("legacy", "secret")
	if err != nil {
		t.Fatal(err)
	}
	user.Role = ""
	user.AreaID = ""
	data, _ := json.Marshal(webUsersFile{Users: []webUser{user}})
	file := filepath.Join(server.Settings.String("", "internal.configdirfull"), "web_users.json")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := server.loadWebUsers(); err != nil {
		t.Fatal(err)
	}
	loaded, _ := server.userByName("legacy")
	if loaded.Role != roleAdmin || loaded.AreaID != user.Salt {
		t.Fatal("legacy migration", loaded)
	}
	if _, ok := server.Settings.Storage().(storage.Filesystem); !ok {
		t.Fatal("unexpected backend")
	}
}

func TestAccessChangeClosesEventStream(t *testing.T) {
	server := fixtureApp(t)
	admin := accessClient{t: t, server: server}
	admin.call("POST", "/api/auth/users/create", `{"username":"admin","password":"secret"}`, 200)
	admin.call("POST", "/api/auth/enabled", `{"enabled":true}`, 200)
	admin = admin.login("admin")
	admin.call("POST", "/api/auth/users/create", `{"username":"alice","password":"secret"}`, 200)
	alice := admin.login("alice")
	endpoint := httptest.NewServer(http.HandlerFunc(server.Management))
	defer endpoint.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, "GET", endpoint.URL+"/api/sse", nil)
	request.Header.Set("Authorization", "Bearer "+alice.token)
	reply, err := endpoint.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer reply.Body.Close()
	if reply.StatusCode != 200 {
		t.Fatal(reply.StatusCode)
	}
	readDone := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, reply.Body); readDone <- err }()
	admin.call("POST", "/api/auth/users/access", `{"username":"alice","role":"reader"}`, 200)
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("revoked event stream remained open")
	}
	alice.call("GET", "/web/../custom_img/private.png", "", 400)
}

func TestConcurrentAreaInitialization(t *testing.T) {
	server := fixtureApp(t)
	user, err := saltedUser("test", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := server.saveWebUsers([]webUser{user}); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	results := make(chan *Server, 16)
	for range 16 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			area, err := server.userArea(user)
			if err != nil {
				t.Error(err)
				return
			}
			results <- area
		}()
	}
	wait.Wait()
	close(results)
	var first *Server
	for area := range results {
		if first == nil {
			first = area
		}
		if first != area {
			t.Fatal("concurrent callers received different areas")
		}
	}
}
