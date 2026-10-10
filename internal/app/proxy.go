// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

var errProxyHeaders = errors.New("invalid reverse proxy headers")

type managementSchemeKey struct{}

func requestScheme(r *http.Request) string {
	if scheme, ok := r.Context().Value(managementSchemeKey{}).(string); ok {
		return scheme
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// managementProxyRequest accepts public scheme/host only from configured direct
// peers. The proxy must overwrite these headers; forwarded chains are rejected.
// Box requests never pass through this function or acquire a proxy identity.
func (s *Server) managementProxyRequest(r *http.Request) (*http.Request, error) {
	proxies := s.Settings.String("", "core.server.trusted_proxies")
	if proxies == "" {
		return r, nil
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r, nil
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return r, nil
	}
	trusted := false
	for _, cidr := range strings.Fields(proxies) {
		prefix, err := netip.ParsePrefix(cidr)
		if err == nil && prefix.Contains(peer.Unmap()) {
			trusted = true
			break
		}
	}
	if !trusted {
		return r, nil
	}
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" && r.Header.Get("X-Forwarded-Host") == "" {
		return r, nil
	}
	if len(r.Header.Values("X-Forwarded-Proto")) != 1 || proto != "http" && proto != "https" {
		return nil, errProxyHeaders
	}
	publicHost := r.Header.Get("X-Forwarded-Host")
	if publicHost == "" {
		publicHost = r.Host
	}
	if len(r.Header.Values("X-Forwarded-Host")) > 1 || !validProxyHost(publicHost) {
		return nil, errProxyHeaders
	}
	r = r.WithContext(context.WithValue(r.Context(), managementSchemeKey{}, proto))
	r.Host = publicHost
	return r, nil
}

func validProxyHost(host string) bool {
	if strings.ContainsAny(host, ",\\ \t\r\n") {
		return false
	}
	u, err := url.Parse("http://" + host)
	if err != nil {
		return false
	}
	return u.Host == host && u.Hostname() != "" && u.User == nil &&
		u.Path == "" && u.RawQuery == "" && u.Fragment == ""
}
