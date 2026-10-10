// SPDX-License-Identifier: GPL-2.0-only
package runtime

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"goteddycloud/internal/boxtransport"
)

const headerPrefix = "X-Goteddy-"

type Peer struct {
	CommonName, Issuer, Subject, RemoteAddr string
	Box, Binary                             bool
}
type peerKey struct{}

func RequestPeer(ctx context.Context) Peer { p, _ := ctx.Value(peerKey{}).(Peer); return p }

type workerInput struct {
	KeyLogBase, KeyLogPath           string
	KeyLogRelay                      bool
	Address, Target, Token           string
	TB1Cert, TB1Key, TB2Cert, TB2Key []byte
}

func certificatePEM(pair tls.Certificate) ([]byte, []byte, error) {
	var chain []byte
	for _, c := range pair.Certificate {
		chain = append(chain, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c})...)
	}
	key, err := x509.MarshalPKCS8PrivateKey(pair.PrivateKey)
	if err != nil {
		return nil, nil, err
	}
	return chain, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), nil
}

// LegacyEnvironment replaces only the TLS signature switch, preserving all other
// GODEBUG settings. Parent and child explicitly select opposite policies.
func LegacyEnvironment(environment []string, enabled bool) []string {
	out := make([]string, 0, len(environment)+1)
	var debug []string
	for _, item := range environment {
		if value, ok := strings.CutPrefix(item, "GODEBUG="); ok {
			for _, setting := range strings.Split(value, ",") {
				if setting != "" && !strings.HasPrefix(setting, "tlssha1=") {
					debug = append(debug, setting)
				}
			}
		} else {
			out = append(out, item)
		}
	}
	value := "0"
	if enabled {
		value = "1"
	}
	return append(out, "GODEBUG="+strings.Join(append(debug, "tlssha1="+value), ","))
}
func StrictParentTLS() error {
	for _, value := range LegacyEnvironment(os.Environ(), false) {
		if strings.HasPrefix(value, "GODEBUG=") {
			return os.Setenv("GODEBUG", strings.TrimPrefix(value, "GODEBUG="))
		}
	}
	return nil
}
func stripInternalHeaders(h http.Header) {
	for k := range h {
		if strings.HasPrefix(strings.ToLower(k), strings.ToLower(headerPrefix)) {
			h.Del(k)
		}
	}
}
func forwardPeer(h http.Header, peer Peer, token string) {
	stripInternalHeaders(h)
	h.Set(headerPrefix+"Token", token)
	h.Set(headerPrefix+"CN", peer.CommonName)
	h.Set(headerPrefix+"Issuer", peer.Issuer)
	h.Set(headerPrefix+"Subject", peer.Subject)
	h.Set(headerPrefix+"Remote", peer.RemoteAddr)
	if peer.Binary {
		h.Set(headerPrefix+"Binary", "true")
	}
}
func peerFromState(ctx context.Context) Peer {
	p := Peer{Box: true}
	if s := boxtransport.State(ctx); s != nil && len(s.PeerCertificates) > 0 {
		c := s.PeerCertificates[0]
		p.CommonName = c.Subject.CommonName
		p.Issuer = c.Issuer.CommonName
		p.Subject = c.Subject.CommonName
	}
	return p
}

// Worker is the private same-executable mode. Its input and lifetime are owned by
// the parent stdin pipe. It never reads production configuration or writes keys.
func Worker(ctx context.Context, input io.Reader, output io.Writer) error {
	decoder := json.NewDecoder(io.LimitReader(input, 1<<20))
	var options workerInput
	if err := decoder.Decode(&options); err != nil {
		return errors.New("invalid box worker configuration")
	}
	target, err := url.Parse(options.Target)
	if err != nil || target.Scheme != "http" || target.Hostname() != "127.0.0.1" || len(options.Token) != 64 {
		return errors.New("invalid private worker endpoint")
	}
	load := func(cert, key []byte) (tls.Certificate, error) { return tls.X509KeyPair(cert, key) }
	tb1, err := load(options.TB1Cert, options.TB1Key)
	if err != nil {
		return err
	}
	tb2, err := load(options.TB2Cert, options.TB2Key)
	if err != nil {
		return err
	}
	policy, err := boxtransport.TLSConfig(tb1, tb2)
	if err != nil {
		return err
	}
	if options.KeyLogRelay {
		policy.KeyLogWriter = &keyLogRelay{target: options.Target, token: options.Token, client: &http.Client{Timeout: 5 * time.Second}}
	}
	listener, err := net.Listen("tcp", options.Address)
	if err != nil {
		return fmt.Errorf("box bind: %w", err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { _, _ = io.Copy(io.Discard, input); cancel() }()
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, MaxConnsPerHost: 32, MaxIdleConnsPerHost: 32, IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: 10 * time.Second}
	defer transport.CloseIdleConnections()
	proxy := &httputil.ReverseProxy{Transport: transport, Rewrite: func(r *httputil.ProxyRequest) {
		r.SetURL(target)
		p := peerFromState(r.In.Context())
		p.RemoteAddr = r.In.RemoteAddr
		forwardPeer(r.Out.Header, p, options.Token)
	}, ErrorHandler: func(w http.ResponseWriter, r *http.Request, e error) {
		http.Error(w, "box backend unavailable", http.StatusBadGateway)
	}}
	if err := json.NewEncoder(output).Encode(map[string]string{"box": listener.Addr().String()}); err != nil {
		return err
	}
	return boxtransport.Serve(ctx, listener, policy, proxy, func(ctx context.Context, frame []byte) error {
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, options.Target+"/_rtnl", strings.NewReader(string(frame)))
		if err != nil {
			return err
		}
		p := peerFromState(ctx)
		p.Binary = true
		forwardPeer(r.Header, p, options.Token)
		response, err := transport.RoundTrip(r)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		if err != nil {
			return err
		}
		if response.StatusCode >= 400 {
			return errors.New("RTNL rejected")
		}
		return nil
	})
}
func privateHandler(token string, device http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get(headerPrefix+"Token")), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		p := Peer{Box: true, CommonName: r.Header.Get(headerPrefix + "CN"), Issuer: r.Header.Get(headerPrefix + "Issuer"), Subject: r.Header.Get(headerPrefix + "Subject"), RemoteAddr: r.Header.Get(headerPrefix + "Remote"), Binary: r.Header.Get(headerPrefix+"Binary") == "true"}
		stripInternalHeaders(r.Header)
		device.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), peerKey{}, p)))
	})
}

// keyLogRelay keeps all durable storage in the parent process. The endpoint is
// authenticated with the same private token as device forwarding.
type keyLogRelay struct {
	target, token string
	client        *http.Client
}

func (k *keyLogRelay) Write(p []byte) (int, error) {
	req, err := http.NewRequest(http.MethodPost, k.target+"/_keylog", strings.NewReader(string(p)))
	if err != nil {
		return 0, err
	}
	req.Header.Set(headerPrefix+"Token", k.token)
	response, err := k.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return 0, errors.New("key log relay failed")
	}
	return len(p), nil
}
