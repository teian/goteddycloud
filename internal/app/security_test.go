// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"crypto/sha1"
	"crypto/tls"
	"encoding/hex"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityLockDiagnostics(t *testing.T) {
	for _, test := range []struct {
		reason, setting, field string
	}{
		{"hard_lock", "security_mit.hardLock", ""},
		{"blacklisted_domain_access", "security_mit.onBlacklistDomain", "internal.security_mit.blacklisted_domain_access"},
		{"crawler_access", "security_mit.onCrawler", "internal.security_mit.crawler_access"},
		{"robots_txt_access", "security_mit.onRobotsTxt", "internal.security_mit.robots_txt_access"},
	} {
		t.Run(test.reason, func(t *testing.T) {
			s := fixtureApp(t)
			if err := s.Settings.Set("", test.setting, "true"); err != nil {
				t.Fatal(err)
			}
			if test.field != "" {
				if err := s.Settings.SetRuntime("", map[string]string{test.field: "123"}); err != nil {
					t.Fatal(err)
				}
			}
			var output bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
			defer slog.SetDefault(previous)
			for range 3 {
				r := httptest.NewRequest("GET", "/private-path?secret-query=value", nil)
				r.Header.Set("Authorization", "secret-auth")
				r.Header.Set("Cookie", "secret-cookie")
				r.Header.Set("User-Agent", "secret-user-agent")
				w := httptest.NewRecorder()
				s.Management(w, r)
				if w.Body.String() != securityLockMessage {
					t.Fatal(w.Body.String())
				}
			}
			log := output.String()
			for _, expected := range []string{"level=WARN", "reason=" + test.reason, "trigger_setting=" + test.setting, "action=locked"} {
				if !strings.Contains(log, expected) {
					t.Fatal("missing security diagnostic", expected, log)
				}
			}
			if strings.Count(log, "TeddyCloud security mitigation triggered") != 1 {
				t.Fatal("repeated security log", log)
			}
			for _, secret := range []string{"private-path", "secret-query", "secret-auth", "secret-cookie", "secret-user-agent"} {
				if strings.Contains(log, secret) {
					t.Fatal("credential disclosed", log)
				}
			}
		})
	}
}

func TestSecurityDiagnosticsWarningThenLock(t *testing.T) {
	s := fixtureApp(t)
	if err := s.Settings.Set("", "security_mit.lockAccess", "false"); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	defer slog.SetDefault(previous)
	r := httptest.NewRequest("GET", "/api/settings/get/internal.security_mit.incident", nil)
	r.Header.Set("User-Agent", "Googlebot")
	w := httptest.NewRecorder()
	s.Management(w, r)
	if w.Body.String() != "true" || !strings.Contains(output.String(), "action=warning") {
		t.Fatal(w.Body.String(), output.String())
	}
	if err := s.Settings.Set("", "security_mit.lockAccess", "true"); err != nil {
		t.Fatal(err)
	}
	r.Header.Del("User-Agent")
	w = httptest.NewRecorder()
	s.Management(w, r)
	if w.Body.String() != securityLockMessage || !strings.Contains(output.String(), "action=locked") {
		t.Fatal(w.Body.String(), output.String())
	}
	if strings.Count(output.String(), "TeddyCloud security mitigation triggered") != 2 {
		t.Fatal(output.String())
	}
}

func TestRobotsAccessIsDiagnosedOnSubsequentLock(t *testing.T) {
	s := fixtureApp(t)
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	defer slog.SetDefault(previous)
	perform(s, "GET", "/robots.txt", nil, false)
	reply := perform(s, "GET", "/api/settings/get/internal.security_mit.incident", nil, false)
	if reply.Body.String() != securityLockMessage || !strings.Contains(output.String(), "reason=robots_txt_access") {
		t.Fatal(reply.Body.String(), output.String())
	}
}

func TestSecurityIncidentWarningAndHTTPSOnly(t *testing.T) {
	s := fixtureApp(t)
	set := func(k, v string) {
		t.Helper()
		if err := s.Settings.Set("", k, v); err != nil {
			t.Fatal(err)
		}
	}
	set("security_mit.onBlacklistDomain", "false")
	set("security_mit.lockAccess", "false")
	r := httptest.NewRequest("GET", "/api/settings/get/internal.security_mit.incident", nil)
	r.Header.Set("User-Agent", "synthetic Googlebot")
	w := httptest.NewRecorder()
	s.Management(w, r)
	if w.Body.String() != "true" || s.Settings.Snapshot("").Uint("internal.security_mit.crawler_access") == 0 {
		t.Fatal(w.Body.String())
	}
	set("security_mit.lockAccess", "true")
	set("security_mit.httpsOnly", "true")
	r.Header.Del("User-Agent")
	w = httptest.NewRecorder()
	s.Management(w, r)
	if w.Body.String() != "true" {
		t.Fatal("HTTP should honor HTTPS-only", w.Body.String())
	}
	r.TLS = &tls.ConnectionState{}
	w = httptest.NewRecorder()
	s.Management(w, r)
	if w.Body.String() != securityLockMessage {
		t.Fatal(w.Body.String())
	}
}
func TestSecurityDomainDetection(t *testing.T) {
	s := fixtureApp(t)
	// Inject a synthetic host hash rather than identifying or contacting any real host.
	hash := sha1.Sum([]byte("synthetic.invalid"))
	key := hex.EncodeToString(hash[:])
	blockedHostHashes[key] = true
	defer delete(blockedHostHashes, key)
	r := httptest.NewRequest("GET", "http://synthetic.invalid:1234/api/settings/get/core.host_url", nil)
	w := httptest.NewRecorder()
	s.Management(w, r)
	if w.Body.String() != securityLockMessage || s.Settings.Snapshot("").Uint("internal.security_mit.blacklisted_domain_access") == 0 {
		t.Fatal(w.Body.String())
	}
}

func TestDevelopIndependentCrawlerDetection(t *testing.T) {
	s := fixtureApp(t)
	r := httptest.NewRequest("GET", "http://synthetic.invalid/api/settings/getIndex", nil)
	r.Header.Set("User-Agent", "synthetic Googlebot")
	w := httptest.NewRecorder()
	s.Management(w, r)
	if w.Body.String() != securityLockMessage || s.Settings.Snapshot("").Uint("internal.security_mit.crawler_access") == 0 {
		t.Fatal(w.Body.String())
	}
}
