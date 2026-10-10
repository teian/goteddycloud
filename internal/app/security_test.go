// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"crypto/sha1"
	"crypto/tls"
	"encoding/hex"
	"net/http/httptest"
	"testing"
)

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
