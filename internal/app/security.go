// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"crypto/sha1"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"

	"goteddycloud/internal/runtime"
)

// These hashes identify hosts; no DNS/network lookup occurs.
var blockedHostHashes = map[string]bool{
	"863a5a96d45d2e8736d1c6a6e1d8b615b6436737": true,
	"26884d3f9c6aa6234390365388a2dc289e750640": true,
	"daf5880dbf7f73bde5d3287d4be22c8e5c1fba21": true,
	"0e71920910bd10d50c22d2494892085d9eb7d7e4": true,
	"1ed20761da4be8801c8a8dc1abd95b7c62db4e87": true,
	"7ee274a4722145a936eaa7dbc1f6f66749758463": true,
}
var crawlerNames = []string{"Googlebot", "bingbot", "yandexbot", "Baiduspider", "Sogou", "Exabot", "ia_archiver", "facebookexternalhit", "Twitterbot", "LinkedInBot", "Embedly", "Quora Link Preview", "showyoubot", "outbrain", "pinterest", "developers.google.com", "Shodan", "Censys", "ZoomEye", "Masscan", "Nmap", "Nuclei", "Nessus", "Acunetix", "Qualys", "Nikto", "Arachni", "Burp Suite", "Netsparker", "w3af", "Zmap", "Digincore", "Netcraft", "SecurityTrails"}

const securityLockMessage = "TeddyCloud has been locked to mitigate security risks! Please check the logs for more information!"

func (s *Server) securityCheck(w http.ResponseWriter, r *http.Request) bool {
	snap := s.Settings.View("")
	https := requestScheme(r) == "https" || runtime.RequestPeer(r.Context()).Box
	now := strconv.FormatInt(s.Now().Unix(), 10)
	if (!snap.Bool("security_mit.httpsOnly") || https) && (snap.Bool("security_mit.warnAccess") || snap.Bool("security_mit.lockAccess")) {
		if snap.Bool("security_mit.onBlacklistDomain") {
			host := r.Host
			// Cyclone strips the optional port when reading the Host header.
			if i := strings.LastIndex(host, ":"); i >= 0 && !strings.Contains(host, "]") {
				host = host[:i]
			}
			sum := sha1.Sum([]byte(host))
			if blockedHostHashes[hex.EncodeToString(sum[:])] {
				_ = s.Settings.SetRuntime("", map[string]string{"internal.security_mit.blacklisted_domain_access": now})
			}
		}
		if snap.Bool("security_mit.onCrawler") {
			for _, name := range crawlerNames {
				if strings.Contains(r.UserAgent(), name) {
					_ = s.Settings.SetRuntime("", map[string]string{"internal.security_mit.crawler_access": now})
					break
				}
			}
		}
	}
	snap = s.Settings.View("")
	if !snap.Bool("security_mit.hardLock") && snap.Bool("security_mit.httpsOnly") && !https {
		return false
	}
	incident := snap.Bool("security_mit.hardLock") && !snap.Bool("internal.config_changed")
	for _, key := range []string{"BlacklistDomain", "Crawler", "RobotsTxt"} {
		field := map[string]string{"BlacklistDomain": "blacklisted_domain_access", "Crawler": "crawler_access", "RobotsTxt": "robots_txt_access"}[key]
		incident = incident || snap.Bool("security_mit.on"+key) && snap.Uint("internal.security_mit."+field) > 0
	}
	if incident {
		_ = s.Settings.SetRuntime("", map[string]string{"internal.security_mit.incident": "true"})
		if snap.Bool("security_mit.lockAccess") {
			response(w, 200, "text/plain", []byte(securityLockMessage))
			return true
		}
	}
	return false
}
