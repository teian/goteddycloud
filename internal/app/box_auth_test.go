// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"goteddycloud/internal/boxstate"
	"goteddycloud/internal/certificates"
	"goteddycloud/internal/protocol"
	serverruntime "goteddycloud/internal/runtime"
)

func TestBoxAuthWorker(t *testing.T) {
	if os.Getenv("GOTEDDY_BOX_AUTH_WORKER") != "1" {
		return
	}
	if err := serverruntime.Worker(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}
func TestBoxCertificateAuthenticationAndRevocation(t *testing.T) {
	s := fixtureApp(t)
	snap := s.Settings.Snapshot("")
	t.Setenv("GOTEDDY_BOX_AUTH_WORKER", "1")
	if err := s.Settings.Set("", "core.boxCertAuth", "false"); err != nil {
		t.Fatal(err)
	} // Legacy setting cannot bypass pinning.
	tb1, err := certificates.Load(snap, "core.server_cert")
	if err != nil {
		t.Fatal(err)
	}
	tb2, err := certificates.Load(snap, "core.server_cert_tb2")
	if err != nil {
		t.Fatal(err)
	}
	load := func(id string) tls.Certificate {
		t.Helper()
		p, err := tls.LoadX509KeyPair(filepath.Join(snap.Base, "certs/client", id, "teddy-cert.pem"), filepath.Join(snap.Base, "certs/client", id, "teddy-key.pem"))
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	pair := load("001122334455")
	roots := x509.NewCertPool()
	cert, err := x509.ParseCertificate(tb1.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots.AddCert(cert)
	ready := make(chan map[string]string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serverruntime.Run(ctx, serverruntime.Options{Box: "127.0.0.1:0", TB1: tb1, TB2: tb2, VerifyBoxCertificate: s.Identity.VerifyCertificate, Management: http.HandlerFunc(s.Management), Device: http.HandlerFunc(s.Device), WorkerCommand: []string{os.Args[0], "-test.run=^TestBoxAuthWorker$"}, Stderr: os.Stderr, Ready: func(v map[string]string) { ready <- v }})
	}()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(8 * time.Second):
			t.Error("worker shutdown timeout")
		}
	}()
	var address string
	select {
	case v := <-ready:
		address = v["box"]
	case err := <-done:
		done <- err
		t.Fatal(err)
	case <-time.After(10 * time.Second):
		t.Fatal("readiness timeout")
	}
	configs := func(pair *tls.Certificate) *tls.Config {
		c := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12}
		if pair != nil {
			c.Certificates = []tls.Certificate{*pair}
		}
		return c
	}
	transport := &http.Transport{TLSClientConfig: configs(&pair), MaxConnsPerHost: 1}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	request := func(status int) {
		t.Helper()
		req, _ := http.NewRequest("POST", "https://"+address+"/v1/freshness-check", nil)
		req.Header.Set("X-Goteddy-CN", "AABBCCDDEEFF")
		req.Header.Set("X-Goteddy-Certificate-SHA256", strings.Repeat("0", 64))
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if status == 200 {
			reply := new(protocol.TonieFreshnessCheckResponse)
			if err := proto.Unmarshal(body, reply); err != nil || reply.GetMaxVolSpk() != 1 {
				t.Fatalf("spoofed identity changed selected box: %v %v", reply, err)
			}
		}
		if res.StatusCode != status {
			t.Fatalf("status %d, want %d", res.StatusCode, status)
		}
	}
	request(200)
	forged := pair
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	template.SignatureAlgorithm = x509.ECDSAWithSHA256
	issuer := *template
	issuer.Subject = template.Issuer
	issuer.PublicKey = key.Public()
	raw, err := x509.CreateCertificate(rand.Reader, template, &issuer, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	forged = tls.Certificate{Certificate: [][]byte{raw}, PrivateKey: key}
	// A second certificate signed with the same key and CA is still unknown.
	renewedTemplate, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	renewedTemplate.SerialNumber = new(big.Int).Add(renewedTemplate.SerialNumber, big.NewInt(1))
	renewedIssuer := *renewedTemplate
	renewedIssuer.Subject = renewedTemplate.Issuer
	renewedDER, err := x509.CreateCertificate(rand.Reader, renewedTemplate, &renewedIssuer, renewedTemplate.PublicKey, pair.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	renewed := pair
	renewed.Certificate = [][]byte{renewedDER}
	wrongKey := pair
	wrongKey.PrivateKey = key
	for _, tc := range []struct {
		name string
		pair *tls.Certificate
	}{{"missing", nil}, {"same key and CA but different certificate", &renewed}, {"forged known CN and issuer", &forged}, {"known certificate without private key", &wrongKey}} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", address, configs(tc.pair))
			if err == nil {
				c.Close()
				t.Fatal("untrusted handshake accepted")
			}
		})
	}
	// Both HTTP keep-alive and RTNL were authenticated before revocation.
	rtnl, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", address, configs(&pair))
	if err != nil {
		t.Fatal(err)
	}
	defer rtnl.Close()
	if err := s.Settings.Set("001122334455", "toniebox.api_access", "false"); err != nil {
		t.Fatal(err)
	}
	request(401)
	_ = rtnl.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = rtnl.Write([]byte{0, 0, 0, 2, 0x12, 0})
	if _, err := rtnl.Read(make([]byte, 1)); err == nil {
		t.Fatal("revoked RTNL connection remained usable")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("revoked RTNL connection did not close")
	}
	if got := s.Boxes.Snapshot("001122334455"); got != (boxstate.State{}) {
		t.Fatal("revoked RTNL produced box state")
	}
	c, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", address, configs(&pair))
	if err == nil {
		c.Close()
		t.Fatal("revoked box reconnected")
	}
}
