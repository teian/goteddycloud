// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type blockingAuthBody struct{ entered, release chan struct{} }

func (body *blockingAuthBody) Read([]byte) (int, error) {
	close(body.entered)
	<-body.release
	return 0, io.EOF
}
func (body *blockingAuthBody) Close() error { return nil }

type blockingAuthWriter struct {
	header           http.Header
	entered, release chan struct{}
}

func (writer *blockingAuthWriter) Header() http.Header { return writer.header }
func (writer *blockingAuthWriter) WriteHeader(int)     {}
func (writer *blockingAuthWriter) Write(data []byte) (int, error) {
	close(writer.entered)
	<-writer.release
	return len(data), nil
}

func TestAuthIOLeavesStateAvailable(t *testing.T) {
	for _, phase := range []string{"request body", "response body"} {
		t.Run(phase, func(t *testing.T) {
			server := fixtureApp(t)
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			request := httptest.NewRequest("POST", "/api/auth/login", nil)
			var writer http.ResponseWriter = httptest.NewRecorder()
			if phase == "request body" {
				request.Body = &blockingAuthBody{entered: entered, release: release}
			} else {
				request = httptest.NewRequest("GET", "/api/auth/status", nil)
				writer = &blockingAuthWriter{header: make(http.Header), entered: entered, release: release}
			}
			go func() { defer close(done); server.Management(writer, request) }()
			defer func() { close(release); <-done }()
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("request did not reach blocked I/O")
			}
			statusDone := make(chan int, 1)
			go func() {
				response := httptest.NewRecorder()
				server.Management(response, httptest.NewRequest("GET", "/api/auth/status", nil))
				statusDone <- response.Code
			}()
			select {
			case status := <-statusDone:
				if status != 200 {
					t.Fatal(status)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("slow I/O held authentication state lock")
			}
		})
	}
}
func TestInvalidWebUsersFailWithoutPublishingState(t *testing.T) {
	valid, err := saltedUser("admin", "secret")
	if err != nil {
		t.Fatal(err)
	}
	duplicate := valid
	duplicate.Username = "ADMIN"
	invalid := valid
	invalid.Salt = "invalid"
	cases := map[string]any{
		"missing array":   map[string]any{},
		"null array":      webUsersFile{},
		"invalid salt":    webUsersFile{Users: []webUser{invalid}},
		"duplicate name":  webUsersFile{Users: []webUser{valid, duplicate}},
		"partially valid": webUsersFile{Users: []webUser{valid, invalid}},
		"too many users":  webUsersFile{Users: make([]webUser, maxWebUsers+1)},
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			server := fixtureApp(t)
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(server.Settings.Snapshot("").String("internal.configdirfull"), "web_users.json")
			if err := os.WriteFile(file, data, 0600); err != nil {
				t.Fatal(err)
			}
			server.auth.users = []webUser{valid}
			if err := server.loadWebUsers(); err == nil {
				t.Fatal("invalid database accepted")
			}
			if len(server.auth.users) != 1 || !reflect.DeepEqual(server.auth.users[0], valid) {
				t.Fatal("failed validation changed users")
			}
			if application, err := New(server.Settings); err == nil {
				application.Close()
				t.Fatal("startup accepted invalid users")
			}
		})
	}
}
func TestWebUserFileRequiresSingleBoundedJSONDocument(t *testing.T) {
	for _, data := range []string{`{"users":[]} {"users":[]}`, strings.Repeat(" ", maxAuthBodyBytes+1)} {
		server := fixtureApp(t)
		file := filepath.Join(server.Settings.Snapshot("").String("internal.configdirfull"), "web_users.json")
		if err := os.WriteFile(file, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if err := server.loadWebUsers(); err == nil {
			t.Fatal("invalid document accepted")
		}
	}
}

type cancellingExportSource struct {
	cancel context.CancelFunc
	reads  int
}

func (source *cancellingExportSource) ReadAt(data []byte, offset int64) (int, error) {
	source.reads++
	clear(data)
	source.cancel()
	return len(data), nil
}
func TestTrackCopyCancelsWithinSingleTrack(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &cancellingExportSource{cancel: cancel}
	err := copyExportSection(ctx, io.Discard, source, 0, maxTrackExportBytes)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("copy error: %v", err)
	}
	if source.reads != 1 {
		t.Fatalf("continued reading after cancellation: %d", source.reads)
	}
}
func TestTrackExportLimitBelongsToServer(t *testing.T) {
	first, second := fixtureApp(t), fixtureApp(t)
	for range maxConcurrentTrackExports {
		first.trackExports <- struct{}{}
	}
	file, err := os.CreateTemp(t.TempDir(), "invalid-taf")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	response := httptest.NewRecorder()
	second.exportTracks(response, httptest.NewRequest("GET", "/content/test?tracks=1", nil), file)
	if response.Code != 400 {
		t.Fatalf("another server consumed export slots: %d", response.Code)
	}
	if len(second.trackExports) != 0 {
		t.Fatal("export slot leaked on failure")
	}
}

func TestConcurrentUserChangesRetainAllSuccessfulWrites(t *testing.T) {
	server := fixtureApp(t)
	admin := accessClient{t: t, server: server}
	admin.call("POST", "/api/auth/users/create", `{"username":"admin","password":"secret"}`, 200)
	admin.call("POST", "/api/auth/enabled", `{"enabled":true}`, 200)
	admin = admin.login("admin")
	const count = 12
	var workers sync.WaitGroup
	statuses := make(chan int, count)
	for index := range count {
		workers.Add(1)
		go func() {
			defer workers.Done()
			request := httptest.NewRequest("POST", "/api/auth/users/create", strings.NewReader(fmt.Sprintf(`{"username":"user%d","password":"secret"}`, index)))
			request.Header.Set("Authorization", "Bearer "+admin.token)
			response := httptest.NewRecorder()
			server.Management(response, request)
			statuses <- response.Code
		}()
	}
	workers.Wait()
	close(statuses)
	for status := range statuses {
		if status != 200 {
			t.Fatalf("create status %d", status)
		}
	}
	reloaded := &Server{Settings: server.Settings}
	if err := reloaded.loadWebUsers(); err != nil {
		t.Fatal(err)
	}
	if len(reloaded.auth.users) != count+1 {
		t.Fatalf("lost concurrent writes: %d users", len(reloaded.auth.users))
	}
}
func TestUserSaveFailureRetainsLiveUsers(t *testing.T) {
	server := fixtureApp(t)
	response := perform(server, "POST", "/api/auth/users/create", []byte(`{"username":"existing","password":"secret"}`), false)
	if response.Code != 200 {
		t.Fatal(response.Code)
	}
	file := filepath.Join(server.Settings.Snapshot("").String("internal.configdirfull"), "web_users.json")
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0700); err != nil {
		t.Fatal(err)
	}
	response = perform(server, "POST", "/api/auth/users/create", []byte(`{"username":"unpublished","password":"secret"}`), false)
	if response.Code != 500 {
		t.Fatal(response.Code)
	}
	server.auth.mu.Lock()
	defer server.auth.mu.Unlock()
	if len(server.auth.users) != 1 || server.auth.users[0].Username != "existing" {
		t.Fatal("failed write published new users")
	}
}
