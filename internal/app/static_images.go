// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"embed"
	"net/http"
	"time"
)

// Upstream teddyCloud placeholders; provenance is recorded in docs/licenses.md.
// These backend URLs are also used by the separately built official frontend.
//
//go:embed static_images/*.png
var defaultImages embed.FS

func serveDefaultImage(w http.ResponseWriter, r *http.Request, path string) bool {
	switch path {
	case "/img_unknown.png", "/img_custom.png", "/img_empty.png":
	default:
		return false
	}
	data, err := defaultImages.ReadFile("static_images" + path)
	if err != nil {
		return false
	}
	w.Header().Set("Content-Type", "image/png")
	http.ServeContent(w, r, path, time.Time{}, bytes.NewReader(data))
	return true
}
