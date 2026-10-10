// SPDX-License-Identifier: GPL-2.0-only
package boxtransport

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goteddycloud/internal/fixtures"
)

func TestTLSIdentityOptionalCertificateAndResumption(t *testing.T) {
	root := filepath.Join(t.TempDir(), "deployment")
	if err := fixtures.Generate(root); err != nil {
		t.Fatal(err)
	}
	load := func(dir string) tls.Certificate {
		cert, err := tls.LoadX509KeyPair(filepath.Join(root, dir, "teddy-cert.pem"), filepath.Join(root, dir, "teddy-key.pem"))
		if err != nil {
			t.Fatal(err)
		}
		return cert
	}
	tb1, tb2 := load("certs/server"), load("certs/server_tb2")
	config, err := TLSConfig(tb1, tb2)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", config)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan error, 1)
	go func() {
		for i := 0; i < 4; i++ {
			conn, err := listener.Accept()
			if err != nil {
				serverDone <- err
				return
			}
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			_, err = conn.Write([]byte{1})
			state := conn.(*tls.Conn).ConnectionState()
			if i == 3 && len(state.PeerCertificates) != 1 {
				err = fmt.Errorf("optional client certificate missing")
			}
			_ = conn.Close()
			if err != nil {
				serverDone <- err
				return
			}
		}
		serverDone <- nil
	}()
	roots := x509.NewCertPool()
	for _, dir := range []string{"certs/server", "certs/server_tb2"} {
		data, err := os.ReadFile(filepath.Join(root, dir, "teddy-cert.pem"))
		if err != nil {
			t.Fatal(err)
		}
		if !roots.AppendCertsFromPEM(data) {
			t.Fatal("fixture CA")
		}
	}
	cache := tls.NewLRUClientSessionCache(2)
	for i, tc := range []struct {
		sni, expected string
		client        bool
		suite         uint16
	}{{"", "Synthetic TB1", false, tls.TLS_RSA_WITH_AES_128_CBC_SHA}, {"box.test", "Synthetic TB2", false, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256}, {"box.test", "Synthetic TB2", false, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256}, {"localhost", "Synthetic TB2", true, tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA}} {
		clientConfig := &tls.Config{MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12, RootCAs: roots, ServerName: tc.sni, CipherSuites: []uint16{tc.suite}, ClientSessionCache: cache}
		if tc.client {
			clientConfig.Certificates = []tls.Certificate{load("certs/client/001122334455")}
		}
		// tls.Dial uses the IP for hostname verification but omits IP SNI.
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", listener.Addr().String(), clientConfig)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		_, err = io.ReadAll(conn)
		if err != nil {
			t.Fatal(err)
		}
		state := conn.ConnectionState()
		_ = conn.Close()
		if state.PeerCertificates[0].Subject.CommonName != tc.expected {
			t.Fatal(state.PeerCertificates[0].Subject.CommonName)
		}
		if state.DidResume {
			t.Fatal("box TLS session resumed without fresh identity")
		}
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}
