// SPDX-License-Identifier: GPL-2.0-only
package cloud

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goteddycloud/internal/config"
	"goteddycloud/internal/fixtures"
)

func TestConfiguredIdentityVerificationHeadersAndCancellation(t *testing.T) {
	base := filepath.Join(t.TempDir(), "fixture")
	if err := fixtures.Generate(base); err != nil {
		t.Fatal(err)
	}
	settings, err := config.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	identity, err := tls.LoadX509KeyPair(filepath.Join(base, "certs/client/001122334455/teddy-cert.pem"), filepath.Join(base, "certs/client/001122334455/teddy-key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(identity.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	seen := make(chan http.Header, 4)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.TLS.PeerCertificates) != 1 || r.TLS.PeerCertificates[0].Subject.CommonName != "001122334455" {
			t.Error("client identity missing")
		}
		if r.URL.Path == "/cancel" {
			<-r.Context().Done()
			return
		}
		seen <- r.Header.Clone()
		io.WriteString(w, "mock cloud")
	}))
	server.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots}
	server.StartTLS()
	defer server.Close()
	target, _ := url.Parse(server.URL)
	host, port, _ := net.SplitHostPort(target.Host)
	snap := settings.Snapshot("")
	snap.Values["cloud.remote_hostname"] = host
	snap.Values["cloud.remote_port"] = port
	snap.Values["core.client_cert.data.ca"] = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	snap.Values["core.client_cert.file.crt"] = "certs/client/001122334455/teddy-cert.pem"
	snap.Values["core.client_cert.file.key"] = "certs/client/001122334455/teddy-key.pem"
	client := New()
	headers := http.Header{"Authorization": {"BD " + strings.Repeat("00", 32)}, "Cookie": {"session=private"}, "X-Goteddy-CN": {"spoofed"}}
	response, err := client.Do(context.Background(), snap, snap, "GET", "/v1/time", "", headers, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if string(data) != "mock cloud" {
		t.Fatal(string(data))
	}
	got := <-seen
	if got.Get("Authorization") == "" || got.Get("Cookie") != "" || got.Get("X-Goteddy-CN") != "" {
		t.Fatal("header allowlist mismatch")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := client.Do(ctx, snap, snap, "GET", "/cancel", "", nil, nil); err == nil {
		t.Fatal("deadline ignored")
	}
	// A different CA must fail without leaking the request's token into diagnostics.
	bad := settings.Snapshot("")
	for k, v := range snap.Values {
		bad.Values[k] = v
	}
	bad.Values["core.client_cert.data.ca"] = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
	if _, err := client.Do(context.Background(), bad, bad, "GET", "/v1/time", "", headers, nil); err == nil || strings.Contains(err.Error(), "BD ") {
		t.Fatal("verification or redaction failed", err)
	}
	if len(client.slots) != 0 {
		t.Fatal("request slots leaked")
	}
}
