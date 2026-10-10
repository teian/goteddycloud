// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Handler benchmarks include request/response allocation but exclude fixture
// setup, networking and TLS. Each path verifies its status on every iteration.
func BenchmarkHandlers(b *testing.B) {
	server := fixtureApp(b)
	for _, tc := range []struct {
		name, method, path string
		device             bool
	}{
		{"Time", "GET", "/v1/time", true},
		{"AuthStatus", "GET", "/api/auth/status", false},
		{"SettingsIndex", "GET", "/api/settings/getIndex", false},
		{"FileIndex", "GET", "/api/fileIndexV2?special=library", false},
		{"TagInfo", "GET", "/api/getTagInfo?ruid=0000000100000001", false},
		{"Freshness", "POST", "/v1/freshness-check", true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				request := httptest.NewRequest(tc.method, tc.path, http.NoBody)
				response := httptest.NewRecorder()
				if tc.device {
					server.Device(response, request)
				} else {
					server.Management(response, request)
				}
				if response.Code != 200 {
					b.Fatalf("%s: %d %s", tc.path, response.Code, response.Body.String())
				}
			}
		})
	}
}
