// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"image/png"
	"net/http"
	"testing"
)

func TestPluginPreviews(t *testing.T) {
	server := fixtureApp(t)
	for _, name := range []string{"goteddycloud-cc3200", "synthetic"} {
		path := "/plugins/" + name + "/preview.png"
		reply := perform(server, http.MethodGet, path, nil, false)
		if reply.Code != http.StatusOK || reply.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("%s: %d %s", path, reply.Code, reply.Header().Get("Content-Type"))
		}
		image, err := png.Decode(bytes.NewReader(reply.Body.Bytes()))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if image.Bounds().Dx() != 320 || image.Bounds().Dy() != 180 {
			t.Fatalf("%s: unexpected dimensions %v", path, image.Bounds())
		}
		if reply := perform(server, http.MethodPost, path, nil, false); reply.Code == http.StatusOK {
			t.Fatalf("%s accepted POST", path)
		}
	}
}
