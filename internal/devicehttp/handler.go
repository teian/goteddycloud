// SPDX-License-Identifier: GPL-2.0-only
package devicehttp

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Handler currently exposes only the offline time reference contract.
// Prefix matching follows src/server.c; unsupported routes remain unavailable.
func Handler(now func() time.Time) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/time") {
			body := strconv.FormatInt(now().Unix(), 10)
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			_, _ = w.Write([]byte(body))
			return
		}
		http.NotFound(w, r)
	})
}
