// SPDX-License-Identifier: GPL-2.0-only
package devicehttp

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestOfflineTimePrefixAndMethods(t *testing.T) {
	h := Handler(func() time.Time { return time.Unix(1700000000, 0) })
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"GET", "/v1/time", 200}, {"GET", "/v1/time/extra", 200}, {"POST", "/v1/time", 404}, {"GET", "/v1/content", 404}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d", tc.method, tc.path, w.Code)
		}
		if tc.status == 200 && (w.Body.String() != "1700000000" || w.Header().Get("Content-Type") != "text/plain; charset=utf-8") {
			t.Fatal(w.Result())
		}
	}
}
