// SPDX-License-Identifier: GPL-2.0-only
package certificates

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"goteddycloud/internal/config"
	"goteddycloud/internal/storage"
	"goteddycloud/internal/teststorage"
)

// A disposable RFC 8555-shaped issuer exercises issuance/challenge/CSR/cache
// integration without contacting a public CA. It does not validate JWS signatures.
func TestACMEIssuanceAndCachedRestart(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(map[bool]string{false: "filesystem", true: "s3"}[remote], func(t *testing.T) {
			caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(365 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
			caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, caKey.Public(), caKey)
			if err != nil {
				t.Fatal(err)
			}
			caCert, err := x509.ParseCertificate(caDER)
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var challengeHandler http.Handler
			var chain []byte
			authorized := false
			var requests atomic.Int64
			var issuer *httptest.Server
			issuer = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := requests.Add(1)
				w.Header().Set("Replay-Nonce", fmt.Sprintf("nonce-%d", n))
				w.Header().Set("Content-Type", "application/json")
				mu.Lock()
				defer mu.Unlock()
				order := func(status string) map[string]any {
					return map[string]any{"status": status, "authorizations": []string{issuer.URL + "/authz"}, "finalize": issuer.URL + "/finalize", "certificate": issuer.URL + "/cert"}
				}
				reply := func(status int, v any) {
					w.WriteHeader(status)
					if err := json.NewEncoder(w).Encode(v); err != nil {
						t.Error(err)
					}
				}
				var payload []byte
				if r.Method == "POST" {
					var jws struct{ Payload string }
					if err := json.NewDecoder(r.Body).Decode(&jws); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					var err error
					payload, err = base64.RawURLEncoding.DecodeString(jws.Payload)
					if err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
				}
				switch r.URL.Path {
				case "/directory":
					reply(200, map[string]any{"newNonce": issuer.URL + "/nonce", "newAccount": issuer.URL + "/account", "newOrder": issuer.URL + "/new-order", "meta": map[string]string{"termsOfService": issuer.URL + "/terms"}})
				case "/nonce":
					w.WriteHeader(200)
				case "/account":
					w.Header().Set("Location", issuer.URL+"/account/1")
					reply(201, map[string]any{"status": "valid", "orders": issuer.URL + "/orders"})
				case "/new-order":
					w.Header().Set("Location", issuer.URL+"/order")
					reply(201, order("pending"))
				case "/authz":
					status := "pending"
					if authorized {
						status = "valid"
					}
					reply(200, map[string]any{"status": status, "identifier": map[string]string{"type": "dns", "value": "teddy.example.net"}, "challenges": []map[string]string{{"type": "http-01", "url": issuer.URL + "/challenge", "token": "integration-token", "status": status}}})
				case "/challenge":
					if challengeHandler == nil {
						t.Error("challenge handler unavailable")
						w.WriteHeader(500)
						return
					}
					rr := httptest.NewRecorder()
					challengeHandler.ServeHTTP(rr, httptest.NewRequest("GET", "http://teddy.example.net/.well-known/acme-challenge/integration-token", nil))
					if rr.Code != 200 || !strings.HasPrefix(rr.Body.String(), "integration-token.") {
						t.Error("HTTP-01 challenge failed", rr.Code, rr.Body.String())
						w.WriteHeader(500)
						return
					}
					authorized = true
					reply(200, map[string]string{"type": "http-01", "status": "valid", "url": issuer.URL + "/challenge", "token": "integration-token"})
				case "/order":
					status := "pending"
					if authorized {
						status = "ready"
					}
					if len(chain) > 0 {
						status = "valid"
					}
					reply(200, order(status))
				case "/finalize":
					var p struct {
						CSR string `json:"csr"`
					}
					if err := json.Unmarshal(payload, &p); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					der, err := base64.RawURLEncoding.DecodeString(p.CSR)
					if err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					csr, err := x509.ParseCertificateRequest(der)
					if err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					if err := csr.CheckSignature(); err != nil {
						t.Error(err)
						w.WriteHeader(400)
						return
					}
					if !authorized || len(csr.DNSNames) != 1 || csr.DNSNames[0] != "teddy.example.net" {
						t.Error("invalid CSR names or authorization")
						w.WriteHeader(400)
						return
					}
					template := &x509.Certificate{SerialNumber: big.NewInt(42), DNSNames: csr.DNSNames, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
					der, err = x509.CreateCertificate(rand.Reader, template, caCert, csr.PublicKey, caKey)
					if err != nil {
						t.Error(err)
						w.WriteHeader(500)
						return
					}
					chain = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
					chain = append(chain, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})...)
					reply(200, order("valid"))
				case "/cert":
					w.Header().Set("Content-Type", "application/pem-certificate-chain")
					w.Write(chain)
				default:
					t.Error("unexpected issuer request", r.Method, r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer issuer.Close()
			base := t.TempDir()
			var backend storage.Backend = storage.Filesystem{}
			if remote {
				backend = teststorage.New(t, base, nil)
			}
			store, err := config.OpenWithStorage(base, backend, false)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			for k, v := range map[string]string{"enabled": "true", "accept_terms": "true", "domains": "teddy.example.net", "directory_url": issuer.URL + "/directory"} {
				if err := store.Set("", "core.httpsecure.acme."+k, v); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			web, err := managementTLS(ctx, store.Snapshot(""), issuer.Client().Transport)
			if err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			challengeHandler = web.HTTPHandler(http.NotFoundHandler())
			mu.Unlock()
			hello := &tls.ClientHelloInfo{ServerName: "teddy.example.net", SignatureSchemes: []tls.SignatureScheme{tls.ECDSAWithP256AndSHA256}, SupportedCurves: []tls.CurveID{tls.CurveP256}, CipherSuites: []uint16{tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256}}
			pair, err := web.Config.GetCertificate(hello)
			if err != nil {
				t.Fatal(err)
			}
			leaf, err := x509.ParseCertificate(pair.Certificate[0])
			if err != nil || leaf.VerifyHostname("teddy.example.net") != nil {
				t.Fatal("wrong issued certificate", err)
			}
			roots := x509.NewCertPool()
			roots.AddCert(caCert)
			if _, err := leaf.Verify(x509.VerifyOptions{DNSName: "teddy.example.net", Roots: roots}); err != nil {
				t.Fatal(err)
			}
			// Count only the restarted manager's I/O. The original manager may
			// still fetch /authz asynchronously to clean up pending authorizations.
			var restartRequests atomic.Int64
			restartTransport := testRoundTripper(func(r *http.Request) (*http.Response, error) {
				restartRequests.Add(1)
				return issuer.Client().Transport.RoundTrip(r)
			})
			fresh, err := managementTLS(ctx, store.Snapshot(""), restartTransport)
			if err != nil {
				t.Fatal(err)
			}
			cached, err := fresh.Config.GetCertificate(hello)
			if err != nil || !bytes.Equal(pair.Certificate[0], cached.Certificate[0]) {
				t.Fatal("cached certificate not reused", err)
			}
			if restartRequests.Load() != 0 {
				t.Fatal("cached restart contacted issuer")
			}
		})
	}
}
