// SPDX-License-Identifier: GPL-2.0-only
package cloud

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"

	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"goteddycloud/internal/config"
)

func material(snap config.Snapshot, key string) ([]byte, error) {
	if raw := snap.String("core.client_cert.data." + key); raw != "" {
		return []byte(raw), nil
	}
	path := snap.String("core.client_cert.file." + key)
	if !filepath.IsAbs(path) {
		path = filepath.Join(snap.Base, path)
	}
	f, err := snap.Storage().Open(path)
	if err != nil {
		return nil, errors.New("upstream client material unavailable")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 1<<20+1))
	if err != nil || len(data) > 1<<20 {
		return nil, errors.New("upstream client material exceeds bounds")
	}
	return data, nil
}
func pemMaterial(kind string, data []byte) []byte {
	if bytes.Contains(data, []byte("-----BEGIN ")) {
		return data
	}
	return pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: data})
}
func clientTLS(snap, global config.Snapshot) (*tls.Config, error) {
	load := func(s config.Snapshot) ([]byte, []byte, []byte, error) {
		ca, a := material(s, "ca")
		crt, b := material(s, "crt")
		key, c := material(s, "key")
		return ca, crt, key, errors.Join(a, b, c)
	}
	ca, crt, key, err := load(snap)
	if err != nil {
		ca, crt, key, err = load(global)
	}
	if err != nil {
		return nil, errors.New("complete upstream CA/client identity required")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pemMaterial("CERTIFICATE", ca)) {
		return nil, errors.New("invalid upstream CA")
	}
	// DER keys may be PKCS8, PKCS1 RSA or SEC1 EC, as in copied deployments.
	if !bytes.Contains(key, []byte("-----BEGIN ")) {
		kind := "PRIVATE KEY"
		if _, err := x509.ParsePKCS1PrivateKey(key); err == nil {
			kind = "RSA PRIVATE KEY"
		} else if _, err := x509.ParseECPrivateKey(key); err == nil {
			kind = "EC PRIVATE KEY"
		}
		key = pemMaterial(kind, key)
	}
	pair, err := tls.X509KeyPair(pemMaterial("CERTIFICATE", crt), key)
	if err != nil {
		return nil, errors.New("invalid upstream client identity")
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, Certificates: []tls.Certificate{pair}}, nil
}

type Client struct{ slots chan struct{} }

func New() *Client { return &Client{slots: make(chan struct{}, 16)} }

type ownedBody struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (b *ownedBody) Close() error { err := b.ReadCloser.Close(); b.once.Do(b.release); return err }

// Do uses only configured TLS targets and an explicit header allowlist. Request
// secrets never appear in errors, and credentials do not cross redirect origins.
func (c *Client) Do(ctx context.Context, snap, global config.Snapshot, method, path, query string, headers http.Header, body []byte) (*http.Response, error) {
	if len(body) > 8<<20 {
		return nil, errors.New("upstream request exceeds limit")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case c.slots <- struct{}{}:
	default:
		return nil, errors.New("upstream concurrency limit reached")
	}
	releaseSlot := func() { <-c.slots }
	policy, err := clientTLS(snap, global)
	if err != nil {
		releaseSlot()
		return nil, err
	}
	suffix := ""
	if strings.HasPrefix(path, "/v3/") {
		suffix = "_tb2"
	}
	host := snap.String("cloud.remote_hostname" + suffix)
	port := snap.Uint("cloud.remote_port" + suffix)
	if host == "" || strings.ContainsAny(host, "/@?#") || port == 0 || port > 65535 {
		releaseSlot()
		return nil, errors.New("invalid upstream target")
	}
	parsed, err := url.ParseRequestURI(path)
	if err != nil || !strings.HasPrefix(path, "/") || parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" {
		releaseSlot()
		return nil, errors.New("invalid upstream path")
	}
	target := url.URL{Scheme: "https", Host: net.JoinHostPort(host, strconv.FormatUint(port, 10)), Path: parsed.Path, RawPath: parsed.RawPath, RawQuery: query}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	request, err := http.NewRequestWithContext(ctx, method, target.String(), bytes.NewReader(body))
	if err != nil {
		cancel()
		releaseSlot()
		return nil, errors.New("invalid upstream request")
	}
	for _, key := range []string{"User-Agent", "Content-Type", "Range", "Authorization"} {
		if value := headers.Get(key); value != "" {
			request.Header.Set(key, value)
		}
	}
	connectTimeout := time.Duration(snap.Uint("core.server.http_client_timeout")) * time.Millisecond
	if connectTimeout < 250*time.Millisecond || connectTimeout > 10*time.Second {
		connectTimeout = 2 * time.Second
	}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: policy, DialContext: (&net.Dialer{Timeout: connectTimeout}).DialContext, TLSHandshakeTimeout: connectTimeout, ResponseHeaderTimeout: 10 * time.Second, DisableKeepAlives: true, MaxResponseHeaderBytes: 32 << 10}
	client := &http.Client{Transport: transport, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("upstream redirect limit")
		}
		if r.URL.Scheme != target.Scheme || r.URL.Host != target.Host {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	response, err := client.Do(request)
	if err != nil {
		cause := ctx.Err()
		transport.CloseIdleConnections()
		cancel()
		releaseSlot()
		if cause != nil {
			return nil, cause
		}
		return nil, fmt.Errorf("upstream TLS or transport failure")
	}
	response.Body = &ownedBody{ReadCloser: response.Body, release: func() { transport.CloseIdleConnections(); cancel(); releaseSlot() }}
	return response, nil
}
