// SPDX-License-Identifier: GPL-2.0-only
package cloud

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goteddycloud/internal/config"
	"goteddycloud/internal/fixtures"
)

func TestTransportDiagnosticsObserveCertificateSelectionWithoutLeakingSecrets(t *testing.T) {
	base := filepath.Join(t.TempDir(), "fixture")
	if err := fixtures.Generate(base); err != nil {
		t.Fatal(err)
	}
	settings, err := config.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	caData, err := os.ReadFile(filepath.Join(base, "certs/client/001122334455/ca.der"))
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caData)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, state                   string
		request, compatible, fallback bool
		level                         slog.Level
	}{
		{"box identity", "sent", true, true, false, slog.LevelDebug},
		{"global fallback", "sent", true, true, true, slog.LevelDebug},
		{"issuer not accepted", "not_sent", true, false, false, slog.LevelDebug},
		{"not requested", "not_requested", false, false, false, slog.LevelDebug},
		{"info hides certificate details", "sent", true, true, false, slog.LevelInfo},
		{"info hides certificate fallback", "sent", true, true, true, slog.LevelInfo},
		{"info hides rejected certificate details", "not_sent", true, false, false, slog.LevelInfo},
	} {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: test.level})).With("cloud_request", 123)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.RawQuery != "secret-query=value" || r.Header.Get("Authorization") != "BD "+strings.Repeat("AB", 32) {
					t.Error("diagnostics changed the request")
				}
				w.Header().Set("WWW-Authenticate", "secret-response-header")
				if len(r.TLS.PeerCertificates) == 0 {
					w.WriteHeader(401)
				}
				_, _ = io.WriteString(w, "secret-response-body")
			}))
			roots := x509.NewCertPool()
			if test.compatible {
				roots.AddCert(ca)
			}
			server.TLS = &tls.Config{ClientCAs: roots}
			if test.request {
				server.TLS.ClientAuth = tls.RequestClientCert
			}
			server.StartTLS()
			defer server.Close()
			if !test.compatible {
				// A nonmatching issuer causes the normal TLS selector to omit the cert.
				roots.AddCert(server.Certificate())
			}
			target, _ := url.Parse(server.URL)
			host, port, _ := net.SplitHostPort(target.Host)
			global := settings.Snapshot("")
			global.Values["core.client_cert.data.ca"] = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
			global.Values["core.client_cert.file.crt"] = "certs/client/001122334455/teddy-cert.pem"
			global.Values["core.client_cert.file.key"] = "certs/client/001122334455/teddy-key.pem"
			snap := settings.Snapshot("001122334455")
			for key, value := range global.Values {
				snap.Values[key] = value
			}
			snap.Values["commonName"] = "001122334455"
			snap.Values["cloud.remote_hostname"] = host
			snap.Values["cloud.remote_port"] = port
			if test.fallback {
				snap.Values["core.client_cert.file.crt"] = "missing-client.der"
			}
			ctx := WithDiagnosticLogger(context.Background(), logger)
			res, err := New().Do(ctx, snap, global, "GET", "/v2/content/97763c1a500304e0", "secret-query=value", http.Header{
				"Authorization": {"BD " + strings.Repeat("AB", 32)}, "User-Agent": {"secret-user-agent"},
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(res.Body)
			res.Body.Close()
			if err != nil || string(body) != "secret-response-body" {
				t.Fatal("response changed", err)
			}
			wantStatus := 401
			if test.state == "sent" {
				wantStatus = 200
			}
			if res.StatusCode != wantStatus {
				t.Fatal("TLS selection changed", res.StatusCode)
			}
			output := logs.String()
			if test.level == slog.LevelInfo {
				if output != "" {
					t.Fatal("transport/certificate details must require debug level", output)
				}
				return
			}
			for _, expected := range []string{"cloud_request=123", "Tonies cloud TLS identity", "certificate_sha256=", "tls_version=", "client_certificate=" + test.state} {
				if !strings.Contains(output, expected) {
					t.Fatalf("missing diagnostic %q: %s", expected, output)
				}
			}
			if test.fallback && !strings.Contains(output, "identity_source=global global_fallback=true") {
				t.Fatal("fallback not visible", output)
			}
			if test.state == "not_sent" && !strings.Contains(output, "reason=issuer_not_accepted") {
				t.Fatal("omitted certificate not explained", output)
			}
			for _, secret := range []string{strings.Repeat("AB", 32), "secret-query", "secret-response", "secret-user-agent", "PRIVATE KEY", "97763c1a500304e0"} {
				if strings.Contains(output, secret) {
					t.Fatal("secret disclosed in diagnostics", secret)
				}
			}
		})
	}
}
