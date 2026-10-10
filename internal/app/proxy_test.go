// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestManagementBehindTLSReverseProxy(t *testing.T) {
	s := fixtureApp(t)
	if err := s.Settings.Set("", "core.server.trusted_proxies", "127.0.0.1/32 ::1/128"); err != nil {
		t.Fatal(err)
	}
	if w := perform(s, "POST", "/api/auth/users/create", []byte(`{"username":"admin","password":"secret"}`), false); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	backend := httptest.NewServer(http.HandlerFunc(s.Management))
	defer backend.Close()
	target, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) {
		r.SetURL(target)
		// Publish the original public origin, overwriting client headers.
		r.Out.Header.Set("X-Forwarded-Proto", "https")
		r.Out.Header.Set("X-Forwarded-Host", r.In.Host)
	}}
	frontend := httptest.NewTLSServer(proxy)
	defer frontend.Close()
	client := frontend.Client()
	login, err := client.Post(frontend.URL+"/api/auth/login", "application/json", strings.NewReader(`{"username":"admin","password":"secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer login.Body.Close()
	cookies := login.Cookies()
	if login.StatusCode != 200 || len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatal(login.StatusCode, cookies)
	}
	req, err := http.NewRequest("POST", frontend.URL+"/api/cc3200/inspect", bytes.NewBufferString("invalid flash"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", frontend.URL)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	// A matching public HTTPS origin reaches input validation through the HTTP backend.
	if resp.StatusCode != 400 {
		t.Fatal("same-origin request rejected", resp.StatusCode)
	}
	req, _ = http.NewRequest("POST", frontend.URL+"/api/cc3200/inspect", nil)
	req.Header.Set("Origin", "https://other.invalid")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("cross-origin request accepted", resp.StatusCode)
	}
	for _, path := range []string{"/v1/time", "/v2/content/test", "/v3/freshness"} {
		resp, err := client.Get(frontend.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatal("management exposed box API", path, resp.StatusCode)
		}
	}
	if w := perform(s, "GET", "/v1/time", nil, true); w.Code != 200 {
		t.Fatal("box API unavailable", w.Code)
	}
}

func TestManagementProxyTrustBoundary(t *testing.T) {
	s := fixtureApp(t)
	for _, trusted := range []string{"", "192.0.2.0/24", "127.0.0.1/32"} {
		if err := s.Settings.Set("", "core.server.trusted_proxies", trusted); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("GET", "http://backend.invalid/api/auth/status", nil)
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-Host", "public.invalid:9443")
		got, err := s.managementProxyRequest(r)
		if err != nil {
			t.Fatal(err)
		}
		expectedScheme, expectedHost := "http", "backend.invalid"
		if trusted == "127.0.0.1/32" {
			expectedScheme, expectedHost = "https", "public.invalid:9443"
		}
		if requestScheme(got) != expectedScheme || got.Host != expectedHost || got.TLS != nil {
			t.Fatal("proxy identity mismatch", requestScheme(got), got.Host)
		}
	}
	for _, headers := range []map[string]string{
		{"X-Forwarded-Proto": "https,http"},
		{"X-Forwarded-Proto": "ftp"},
		{"X-Forwarded-Host": "public.invalid"},
		{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "public.invalid,other.invalid"},
		{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "public.invalid/path"},
		{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "user@public.invalid"},
	} {
		r := httptest.NewRequest("GET", "http://backend.invalid/", nil)
		r.RemoteAddr = "127.0.0.1:12345"
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		if _, err := s.managementProxyRequest(r); err == nil {
			t.Fatal("accepted invalid proxy headers", headers)
		}
	}
	if err := s.Settings.Set("", "core.server.trusted_proxies", "not-a-cidr"); err == nil {
		t.Fatal("accepted invalid proxy configuration")
	}
}

func TestManagementClockKeepsDeviceRoutesSeparate(t *testing.T) {
	s := fixtureApp(t)
	s.Now = func() time.Time { return time.Unix(1700000000, 0) }
	reply := perform(s, "GET", "/api/time", nil, false)
	if reply.Code != http.StatusOK || reply.Body.String() != "1700000000" {
		t.Fatalf("management clock: %d %q", reply.Code, reply.Body.String())
	}
	for _, path := range []string{"/v1/time", "/api/time/extra"} {
		if reply := perform(s, "GET", path, nil, false); reply.Code != http.StatusNotFound {
			t.Fatalf("%s: %d", path, reply.Code)
		}
	}
	if reply := perform(s, "POST", "/api/time", nil, false); reply.Code == http.StatusOK {
		t.Fatal("clock accepted POST")
	}
	if reply := perform(s, "GET", "/api/time", nil, true); reply.Code != http.StatusNotFound {
		t.Fatalf("management clock exposed on box listener: %d", reply.Code)
	}
}
