// SPDX-License-Identifier: GPL-2.0-only
package certificates

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"

	"goteddycloud/internal/config"
	"goteddycloud/internal/storage"
)

// WebTLS configures management HTTPS independently of device TLS. Construction
// performs no ACME requests; issuance begins on an allowed TLS handshake.
type WebTLS struct {
	Config      *tls.Config
	HTTPHandler func(http.Handler) http.Handler
}

func ManagementTLS(ctx context.Context, snap config.Snapshot) (*WebTLS, error) {
	return managementTLS(ctx, snap, http.DefaultTransport)
}

func managementTLS(ctx context.Context, snap config.Snapshot, transport http.RoundTripper) (*WebTLS, error) {
	if err := validateWebTLSOptions(snap); err != nil {
		return nil, err
	}
	enabled := snap.Bool("core.httpsecure.acme.enabled")
	if snap.String("core.httpsecure.certificate_file") != "" {
		pair, err := loadManagementPair(snap)
		if err != nil {
			return nil, err
		}
		return &WebTLS{Config: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}}}, nil
	}
	if !enabled {
		pair, err := Ensure(snap, "core.server_cert")
		if err != nil {
			return nil, err
		}
		return &WebTLS{Config: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}}}, nil
	}
	domains := strings.Fields(snap.String("core.httpsecure.acme.domains"))
	directory := snap.String("core.httpsecure.acme.directory_url")
	cacheDir := snap.String("core.httpsecure.acme.cache_dir")
	if !filepath.IsAbs(cacheDir) {
		cacheDir = filepath.Join(snap.Base, cacheDir)
	}
	// Accounts and certificates from staging/private CAs must not mix with production.
	namespace := sha256.Sum256([]byte(directory))
	cacheDir = filepath.Join(cacheDir, hex.EncodeToString(namespace[:16]))
	if err := snap.Storage().MkdirAll(cacheDir, 0700); err != nil {
		return nil, err
	}
	manager := &autocert.Manager{
		Prompt:     autocert.AcceptTOS,
		HostPolicy: autocert.HostWhitelist(domains...),
		Email:      snap.String("core.httpsecure.acme.email"),
		Cache:      &acmeCache{backend: snap.Storage(), directory: cacheDir, lifetime: ctx},
		Client:     &acme.Client{DirectoryURL: directory, HTTPClient: &http.Client{Timeout: 2 * time.Minute, Transport: lifetimeTransport{ctx, transport}}},
	}
	tlsConfig := manager.TLSConfig()
	getCertificate := tlsConfig.GetCertificate
	tlsConfig.GetCertificate = func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := manager.HostPolicy(ctx, strings.ToLower(hello.ServerName)); err != nil {
			return nil, err
		}
		return getCertificate(hello)
	}
	tlsConfig.MinVersion = tls.VersionTLS12
	// runtime serves an explicit TLS listener with HTTP/1.1; do not advertise h2.
	tlsConfig.NextProtos = []string{"http/1.1", acme.ALPNProto}
	return &WebTLS{Config: tlsConfig, HTTPHandler: manager.HTTPHandler}, nil
}

// ValidateManagementTLS checks configuration and manual material without writes,
// certificate generation, or ACME network traffic.
func ValidateManagementTLS(snap config.Snapshot) error {
	if err := validateWebTLSOptions(snap); err != nil {
		return err
	}
	if snap.String("core.httpsecure.certificate_file") != "" {
		_, err := loadManagementPair(snap)
		return err
	}
	return nil
}

func loadManagementPair(snap config.Snapshot) (tls.Certificate, error) {
	certificate, key := snap.String("core.httpsecure.certificate_file"), snap.String("core.httpsecure.key_file")
	resolve := func(name string) string {
		if filepath.IsAbs(name) {
			return name
		}
		return filepath.Join(snap.Base, name)
	}
	pair, err := loadPair(snap, resolve(certificate), resolve(key))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("management TLS key pair: %w", err)
	}
	return pair, nil
}

func validateWebTLSOptions(snap config.Snapshot) error {
	certificate, key := snap.String("core.httpsecure.certificate_file"), snap.String("core.httpsecure.key_file")
	enabled := snap.Bool("core.httpsecure.acme.enabled")
	if certificate != "" || key != "" {
		if enabled {
			return errors.New("management TLS: manual certificates and ACME are mutually exclusive")
		}
		if certificate == "" || key == "" {
			return errors.New("management TLS: certificate_file and key_file are both required")
		}
		return nil
	}
	if !enabled {
		return nil
	}
	if !snap.Bool("core.httpsecure.acme.accept_terms") {
		return errors.New("management ACME requires accept_terms=true")
	}
	domains := strings.Fields(snap.String("core.httpsecure.acme.domains"))
	if len(domains) == 0 {
		return errors.New("management ACME requires explicit domains")
	}
	for _, domain := range domains {
		if !validACMEDomain(domain) {
			return fmt.Errorf("invalid ACME domain %q; use an ASCII DNS name", domain)
		}
	}
	directory := snap.String("core.httpsecure.acme.directory_url")
	u, err := url.Parse(directory)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return errors.New("ACME directory_url must be an HTTPS URL")
	}
	cacheDir := snap.String("core.httpsecure.acme.cache_dir")
	if cacheDir == "" {
		return errors.New("ACME cache_dir is required")
	}
	return nil
}

func validACMEDomain(domain string) bool {
	if len(domain) > 253 || !strings.Contains(domain, ".") || net.ParseIP(domain) != nil {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// acmeCache uses the deployment backend for account keys and certificate chains.
// Opaque cache keys become single hashed filenames to prevent path traversal.
// Roots are opened per operation, so renewal does not retain closed root handles.
type acmeCache struct {
	backend   storage.Backend
	directory string
	lifetime  context.Context
}

func (c *acmeCache) root(ctx context.Context, key string) (*storage.Root, string, error) {
	if err := c.lifetime.Err(); err != nil {
		return nil, "", err
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256([]byte(key))
	root, err := c.backend.OpenRoot(c.directory)
	return root, hex.EncodeToString(sum[:]), err
}
func (c *acmeCache) Get(ctx context.Context, key string) ([]byte, error) {
	ctx, cancel := withLifetime(c.lifetime, ctx)
	defer cancel()
	root, name, err := c.root(ctx, key)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.WithContext(ctx).Open(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, autocert.ErrCacheMiss
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxMaterial+1))
	if err == nil && len(data) > MaxMaterial {
		return nil, errors.New("ACME cache entry exceeds size limit")
	}
	return data, err
}
func (c *acmeCache) Put(ctx context.Context, key string, data []byte) error {
	ctx, cancel := withLifetime(c.lifetime, ctx)
	defer cancel()
	if len(data) > MaxMaterial {
		return errors.New("ACME cache entry exceeds size limit")
	}
	root, name, err := c.root(ctx, key)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.WithContext(ctx).WriteReaderAtomic(ctx, name, bytes.NewReader(data), 0600, int64(len(data)))
}
func (c *acmeCache) Delete(ctx context.Context, key string) error {
	ctx, cancel := withLifetime(c.lifetime, ctx)
	defer cancel()
	root, name, err := c.root(ctx, key)
	if err != nil {
		return err
	}
	defer root.Close()
	err = root.WithContext(ctx).Remove(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// autocert's renewal requests use background contexts. Tie their network I/O to
// this server lifetime as well, including response reads, across in-process restarts.
type lifetimeTransport struct {
	ctx  context.Context
	base http.RoundTripper
}

func (t lifetimeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := t.ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cleanup := withLifetime(t.ctx, r.Context())
	resp, err := t.base.RoundTrip(r.WithContext(ctx))
	if err != nil {
		cleanup()
		return nil, err
	}
	resp.Body = &lifetimeBody{ReadCloser: resp.Body, cleanup: cleanup}
	return resp, nil
}

type lifetimeBody struct {
	io.ReadCloser
	cleanup func()
}

func (b *lifetimeBody) Close() error { defer b.cleanup(); return b.ReadCloser.Close() }

func withLifetime(lifetime, request context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(request)
	stop := context.AfterFunc(lifetime, cancel)
	return ctx, func() { stop(); cancel() }
}
