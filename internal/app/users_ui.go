// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	_ "embed"
	"net/http"
)

//go:embed shares.html
var sharesPage []byte

//go:embed shares.mjs
var sharesScript []byte

func (s *Server) usersUI(w http.ResponseWriter, r *http.Request) bool {
	var data []byte
	kind := "text/html; charset=utf-8"
	switch r.URL.Path {
	case "/tools/shares":
		data = sharesPage
	case "/tools/shares.mjs":
		data = sharesScript
		kind = "text/javascript; charset=utf-8"

	case "/tools/users", "/plugins/goteddycloud-users/index.html", "/plugins/goteddycloud-users/":
		if r.Method != http.MethodGet {
			response(w, http.StatusMethodNotAllowed, "", nil)
			return true
		}
		// Relative redirects retain a reverse proxy's external path prefix.
		target := "../web/settings/users"
		if r.URL.Path != "/tools/users" {
			target = "../../web/settings/users"
		}
		w.Header().Set("Location", target)
		w.WriteHeader(http.StatusFound)
		return true
	case "/plugins/goteddycloud-users/plugin.json", "/plugins/goteddycloud-users/preview.png", "/tools/users.mjs":
		referenceNotFound(w)
		return true
	default:
		return false
	}
	if r.Method != "GET" {
		response(w, 405, "", nil)
		return true
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'self'; base-uri 'none'; form-action 'self'")
	response(w, 200, kind, data)
	return true
}
