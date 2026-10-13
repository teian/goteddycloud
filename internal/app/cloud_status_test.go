// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goteddycloud/internal/config"
)

type statusCloud func(context.Context, http.Header) (*http.Response, error)

func (f statusCloud) Do(ctx context.Context, _, _ config.Snapshot, method, path, query string, headers http.Header, body []byte) (*http.Response, error) {
	if method != "GET" || path != "/v1/time" || query != "" || len(body) != 0 {
		return nil, errors.New("unexpected status probe")
	}
	return f(ctx, headers)
}

func TestCloudStatusReportsAvailabilityWithoutHTTPFailures(t *testing.T) {
	s := fixtureApp(t)
	for _, test := range []struct {
		name, payload string
		enabled       bool
		code          int
		fail          bool
		available     bool
	}{
		{"disabled", "", false, 200, false, false},
		{"online", "1800000000\n", true, 200, false, true},
		{"missing identity or transport failure", "", true, 0, true, false},
		{"rejected", "1800000000", true, 403, false, false},
		{"invalid clock", "not a timestamp", true, 200, false, false},
		{"oversized", strings.Repeat(" ", 65) + "1800000000", true, 200, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := s.Settings.Set("", "cloud.enabled", map[bool]string{true: "true", false: "false"}[test.enabled]); err != nil {
				t.Fatal(err)
			}
			calls := 0
			s.Cloud = statusCloud(func(ctx context.Context, headers http.Header) (*http.Response, error) {
				calls++
				if headers.Get("Authorization") != "" || headers.Get("Cookie") != "" {
					t.Fatal("management credentials forwarded to cloud")
				}
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 5*time.Second {
					t.Fatal("unbounded status probe")
				}
				if test.fail {
					return nil, errors.New("private upstream information")
				}
				return &http.Response{StatusCode: test.code, Body: io.NopCloser(strings.NewReader(test.payload))}, nil
			})
			req := httptest.NewRequest("GET", "/api/cloud/status", nil)
			req.Header.Set("Authorization", "Bearer synthetic-management-token")
			req.Header.Set("Cookie", "session=private-management-cookie")
			w := httptest.NewRecorder()
			s.Management(w, req)
			var status map[string]bool
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &status) != nil || len(status) != 2 || status["enabled"] != test.enabled || status["available"] != test.available {
				t.Fatal(w.Code, w.Body.String())
			}
			if (calls == 1) != test.enabled || calls > 1 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("unexpected probe count or caching", calls, w.Header())
			}
		})
	}
}

func TestCloudStatusKeepsLoginProtectionAndAllowsReaders(t *testing.T) {
	s := fixtureApp(t)
	admin := accessClient{t: t, server: s}
	admin.call("POST", "/api/auth/users/create", `{"username":"admin","password":"secret"}`, 200)
	admin.call("POST", "/api/auth/enabled", `{"enabled":true}`, 200)
	admin = admin.login("admin")
	admin.call("POST", "/api/auth/users/create", `{"username":"reader","password":"secret","role":"reader"}`, 200)
	anonymous := accessClient{t: t, server: s}
	anonymous.call("GET", "/api/cloud/status", "", 401)
	reader := anonymous.login("reader")
	reader.call("GET", "/api/cloud/status", "", 200)
	reader.call("GET", "/api/cloud/status/extra", "", 403)
	reader.call("POST", "/api/cloud/status", "", 403)
	reader.call("GET", "/api/settings/get/core.client_cert.data.key", "", 403)
}
