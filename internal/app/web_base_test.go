// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebBaseForDeepRoutesAndProxyPrefixes(t *testing.T) {
	s := fixtureApp(t)
	file := filepath.Join(s.Settings.String("", "internal.wwwdirfull"), "web", "index.html")
	if err := os.WriteFile(file, []byte("<html><head></head><body></body></html>"), 0600); err != nil {
		t.Fatal(err)
	}
	for path, base := range map[string]string{
		"/web":                     "web/",
		"/web/":                    "./",
		"/web/index.html":          "./",
		"/web/community/tcplugins": ".././",
		"/web/settings/users":      ".././",
		"/web/community/":          ".././",
	} {
		reply := perform(s, http.MethodGet, path, nil, false)
		if reply.Code != 200 || !strings.Contains(reply.Body.String(), "<head><base href=\""+base+"\">") {
			t.Fatalf("%s: %d %s", path, reply.Code, reply.Body.String())
		}
		for _, prefix := range []string{"", "/proxy/instance"} {
			page, _ := url.Parse("https://example.test" + prefix + path)
			relative, _ := url.Parse(base)
			if got := page.ResolveReference(relative).Path; got != prefix+"/web/" {
				t.Fatalf("%s%s resolves to %s", prefix, path, got)
			}
		}
	}
}
