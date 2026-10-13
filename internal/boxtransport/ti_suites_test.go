// SPDX-License-Identifier: GPL-2.0-only
package boxtransport

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"testing"
	"time"
)

// Force permitted TI SDK suites and reject RC4, including certificate
// verification. Merely putting an IANA ID in CipherSuites proves no support.
func TestTISDKCipherHandshakes(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		suite  uint16
		ecdsa  bool
		reject bool
	}{
		{tls.TLS_RSA_WITH_RC4_128_SHA, false, true},
		{tls.TLS_RSA_WITH_AES_256_CBC_SHA, false, false},
		{tls.TLS_ECDHE_RSA_WITH_RC4_128_SHA, false, true},
		{tls.TLS_ECDHE_ECDSA_WITH_RC4_128_SHA, true, true},
		{tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA, false, false},
		{tls.TLS_RSA_WITH_AES_128_CBC_SHA256, false, false},
		{tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256, false, false},
		{tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256, true, false},
	} {
		t.Run(tls.CipherSuiteName(tc.suite), func(t *testing.T) {
			var key any = rsaKey
			var public any = &rsaKey.PublicKey
			if tc.ecdsa {
				key = ecKey
				public = &ecKey.PublicKey
			}
			template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "TI suite test"}, DNSNames: []string{"box.test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
			der, err := x509.CreateCertificate(rand.Reader, template, template, public, key)
			if err != nil {
				t.Fatal(err)
			}
			cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
			config, err := TLSConfig(cert, cert, func(raw []byte) error {
				if !bytes.Equal(raw, der) {
					return errors.New("untrusted")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			listener, err := tls.Listen("tcp", "127.0.0.1:0", config)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(3 * time.Second))
				_, err = conn.Write([]byte("ok"))
				done <- err
			}()
			roots := x509.NewCertPool()
			parsed, err := x509.ParseCertificate(der)
			if err != nil {
				t.Fatal(err)
			}
			roots.AddCert(parsed)
			conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 3 * time.Second}, "tcp", listener.Addr().String(), &tls.Config{MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12, RootCAs: roots, Certificates: []tls.Certificate{cert}, ServerName: "box.test", CipherSuites: []uint16{tc.suite}})
			if tc.reject {
				if err == nil {
					conn.Close()
					t.Fatal("RC4 handshake accepted")
				}
				if serverErr := <-done; serverErr == nil {
					t.Fatal("server accepted RC4")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(3 * time.Second))
			data, err := io.ReadAll(conn)
			if err != nil || string(data) != "ok" {
				t.Fatalf("delivery %q: %v", data, err)
			}
			if conn.ConnectionState().CipherSuite != tc.suite {
				t.Fatal("negotiated another suite")
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}
