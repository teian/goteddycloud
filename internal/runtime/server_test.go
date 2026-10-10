// SPDX-License-Identifier: GPL-2.0-only
package runtime

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goteddycloud/internal/diagnostics"
	"goteddycloud/internal/fixtures"
)

func TestWorkerHelper(t *testing.T) {
	if os.Getenv("GOTEDDY_WORKER_TEST") != "1" {
		return
	}
	if err := Worker(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}
func testOptions(t *testing.T) Options {
	t.Helper()
	root := filepath.Join(t.TempDir(), "fixture")
	if err := fixtures.Generate(root); err != nil {
		t.Fatal(err)
	}
	load := func(dir string) tls.Certificate {
		p, e := tls.LoadX509KeyPair(filepath.Join(root, dir, "teddy-cert.pem"), filepath.Join(root, dir, "teddy-key.pem"))
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	t.Setenv("GOTEDDY_WORKER_TEST", "1")
	return Options{HTTP: "127.0.0.1:0", Web: "127.0.0.1:0", Box: "127.0.0.1:0", TB1: load("certs/server"), TB2: load("certs/server_tb2"), WorkerCommand: []string{os.Args[0], "-test.run=^TestWorkerHelper$"}, Stderr: os.Stderr,
		Management: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "management") }), Device: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := RequestPeer(r.Context())
			if p.Binary {
				fmt.Fprint(w, "frame")
				return
			}
			fmt.Fprintf(w, "box=%t cn=%s forged=%s", p.Box, p.CommonName, r.Header.Get(headerPrefix+"CN"))
		})}
}
func TestListenersIdentityIsolationAndShutdown(t *testing.T) {
	o := testOptions(t)
	o.KeyLogBase = t.TempDir()
	o.KeyLogPath = "keys.log"
	keylog, err := diagnostics.Open(o.KeyLogBase, o.KeyLogPath, 4<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer keylog.Close()
	o.KeyLogWriter = keylog
	ready := make(chan map[string]string, 1)
	o.Ready = func(v map[string]string) { ready <- v }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, o) }()
	var addresses map[string]string
	select {
	case addresses = <-ready:
	case err := <-done:
		t.Fatal(err)
	case <-time.After(10 * time.Second):
		t.Fatal("readiness timeout")
	}
	roots := x509.NewCertPool()
	for _, p := range []tls.Certificate{o.TB1, o.TB2} {
		for _, raw := range p.Certificate {
			c, err := x509.ParseCertificate(raw)
			if err != nil {
				t.Fatal(err)
			}
			roots.AddCert(c)
		}
	}
	httpClient := &http.Client{Timeout: 3 * time.Second}
	resp, err := httpClient.Get("http://" + addresses["http"] + "/api/settings/getIndex")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "management" {
		t.Fatal(string(body))
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	request, _ := http.NewRequest("GET", "https://"+addresses["box"]+"/v1/time", nil)
	request.Header.Set(headerPrefix+"CN", "spoofed")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if string(body) != "box=true cn= forged=" {
		t.Fatal(string(body))
	}
	response, err = client.Get("https://" + addresses["web"] + "/api/settings/getIndex")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	response.Body.Close()
	if string(body) != "management" {
		t.Fatal(string(body))
	}
	keydata, err := os.ReadFile(filepath.Join(o.KeyLogBase, o.KeyLogPath))
	if err != nil {
		t.Fatal(err)
	}
	randoms := map[string]bool{}
	for _, line := range strings.Split(string(keydata), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 {
			randoms[fields[1]] = true
		}
	}
	if len(randoms) != 2 {
		t.Fatalf("expected web and box TLS secrets, got %d handshakes", len(randoms))
	}
	idle, err := net.Dial("tcp", addresses["box"])
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown timeout")
	}
	for name, address := range addresses {
		c, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err == nil {
			c.Close()
			t.Fatalf("%s still accepting", name)
		}
	}
}
func TestBindFailureReleasesEarlierListeners(t *testing.T) {
	o := testOptions(t)
	available, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := available.Addr().String()
	available.Close()
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	o.HTTP = address
	o.Web = occupied.Addr().String()
	if err := Run(context.Background(), o); err == nil {
		t.Fatal("expected web bind failure")
	}
	l, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal("HTTP leaked", err)
	}
	l.Close()
	o.Web = ""
	o.Box = occupied.Addr().String()
	if err := Run(context.Background(), o); err == nil {
		t.Fatal("expected child bind failure")
	}
	l, err = net.Listen("tcp", address)
	if err != nil {
		t.Fatal("HTTP leaked after child failure", err)
	}
	l.Close()
}
func TestLegacyFlagIsScoped(t *testing.T) {
	in := []string{"A=1", "GODEBUG=http2client=0,tlssha1=1"}
	parent := LegacyEnvironment(in, false)
	child := LegacyEnvironment(parent, true)
	if strings.Join(parent, ";") != "A=1;GODEBUG=http2client=0,tlssha1=0" || strings.Join(child, ";") != "A=1;GODEBUG=http2client=0,tlssha1=1" {
		t.Fatal(parent, child)
	}
}

func TestCertificateIdentityUsesOnlyCommonNames(t *testing.T) {
	options := testOptions(t)
	ready := make(chan map[string]string, 1)
	options.Ready = func(addresses map[string]string) { ready <- addresses }
	options.Device = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer := RequestPeer(r.Context())
		fmt.Fprintf(w, "%s|%s|%s", peer.CommonName, peer.Issuer, peer.Subject)
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, options) }()
	defer func() { cancel(); <-done }()
	var addresses map[string]string
	select {
	case addresses = <-ready:
	case err := <-done:
		done <- err
		t.Fatal(err)
	case <-time.After(10 * time.Second):
		t.Fatal("readiness")
	}
	roots := x509.NewCertPool()
	cert, err := x509.ParseCertificate(options.TB1.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots.AddCert(cert)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, Certificates: []tls.Certificate{options.TB1}, MinVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	response, err := client.Get("https://" + addresses["box"] + "/v1/time")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "Synthetic TB1|Synthetic TB1|Synthetic TB1" {
		t.Fatalf("identity included distinguished-name fields: %s", body)
	}
}

func TestIndependentManagementTLSAndHTTPChallenge(t *testing.T) {
	o := testOptions(t)
	// No device certificates are needed for a management-only deployment.
	webPair := o.TB2
	o.TB1, o.TB2 = tls.Certificate{}, tls.Certificate{}
	o.Box = ""
	o.WebTLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{webPair}}
	o.HTTPChallenge = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/.well-known/acme-challenge/test" {
				fmt.Fprint(w, "challenge")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	ready := make(chan map[string]string, 1)
	o.Ready = func(a map[string]string) { ready <- a }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, o) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("shutdown timeout")
		}
	}()
	var addresses map[string]string
	select {
	case addresses = <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("startup timeout")
	}
	roots := x509.NewCertPool()
	for _, der := range webPair.Certificate {
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		roots.AddCert(cert)
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	for _, tc := range []struct{ url, want string }{
		{"http://" + addresses["http"] + "/.well-known/acme-challenge/test", "challenge"},
		{"http://" + addresses["http"] + "/api/auth/status", "management"},
		{"https://" + addresses["web"] + "/.well-known/acme-challenge/test", "management"},
	} {
		resp, err := client.Get(tc.url)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || string(data) != tc.want {
			t.Fatal(tc.url, string(data), err)
		}

	}
}
