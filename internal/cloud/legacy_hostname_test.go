// SPDX-License-Identifier: GPL-2.0-only
package cloud

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"math/big"
	"testing"
	"time"
)

func TestLegacyBoxineHostnameRequiresTrustedValidServer(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                                     string
		leaf, ca                                 func(*x509.Certificate)
		unknownRoot, wrongTarget, tamper, accept bool
	}{
		{name: "exact legacy CN", accept: true},
		{name: "case-insensitive DNS CN", leaf: func(c *x509.Certificate) { c.Subject.CommonName = "PROD.DE.TBS.TOYS" }, accept: true},
		{name: "modern SAN", leaf: func(c *x509.Certificate) {
			c.Subject.CommonName = "other.invalid"
			c.DNSNames = []string{legacyBoxineHost}
		}, accept: true},
		{name: "wrong CN", leaf: func(c *x509.Certificate) { c.Subject.CommonName = "attacker.invalid" }},
		{name: "wildcard CN", leaf: func(c *x509.Certificate) { c.Subject.CommonName = "*.de.tbs.toys" }},
		{name: "conflicting SAN", leaf: func(c *x509.Certificate) { c.DNSNames = []string{"attacker.invalid"} }},
		{name: "email SAN still forbids CN fallback", leaf: func(c *x509.Certificate) { c.EmailAddresses = []string{"test@example.invalid"} }},
		{name: "expired", leaf: func(c *x509.Certificate) { c.NotAfter = time.Now().Add(-time.Minute) }},
		{name: "future", leaf: func(c *x509.Certificate) { c.NotBefore = time.Now().Add(time.Minute) }},
		{name: "client-only EKU", leaf: func(c *x509.Certificate) { c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth} }},
		{name: "unknown critical extension", leaf: func(c *x509.Certificate) {
			c.ExtraExtensions = []pkix.Extension{{Id: asn1.ObjectIdentifier{1, 2, 3, 4, 5}, Critical: true, Value: []byte{5, 0}}}
		}},
		{name: "unknown CA", unknownRoot: true},
		{name: "other configured target", wrongTarget: true},
		{name: "bad signature", tamper: true},
		{name: "expired CA", ca: func(c *x509.Certificate) { c.NotAfter = time.Now().Add(-time.Minute) }},
		{name: "CA name constraints", ca: func(c *x509.Certificate) {
			c.PermittedDNSDomainsCritical = true
			c.PermittedDNSDomains = []string{"attacker.invalid"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Synthetic cloud CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), BasicConstraintsValid: true, IsCA: true, KeyUsage: x509.KeyUsageCertSign}
			if tc.ca != nil {
				tc.ca(ca)
			}
			caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, key.Public(), key)
			if err != nil {
				t.Fatal(err)
			}
			ca, err = x509.ParseCertificate(caDER)
			if err != nil {
				t.Fatal(err)
			}
			leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: legacyBoxineHost}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
			if tc.leaf != nil {
				tc.leaf(leaf)
			}
			der, err := x509.CreateCertificate(rand.Reader, leaf, ca, key.Public(), key)
			if err != nil {
				t.Fatal(err)
			}
			leaf, err = x509.ParseCertificate(der)
			if err != nil {
				t.Fatal(err)
			}
			if tc.tamper {
				leaf.Signature[0] ^= 1
			}
			roots := x509.NewCertPool()
			if !tc.unknownRoot {
				roots.AddCert(ca)
			}
			state := tls.ConnectionState{ServerName: legacyBoxineHost, PeerCertificates: []*x509.Certificate{leaf}}
			if tc.wrongTarget {
				state.ServerName = "attacker.invalid"
			}
			err = verifyBoxineServer(roots)(state)
			if (err == nil) != tc.accept {
				t.Fatalf("accepted=%v, want=%v; error=%v", err == nil, tc.accept, err)
			}
		})
	}
	if verifyBoxineServer(nil)(tls.ConnectionState{}) == nil {
		t.Fatal("missing server identity accepted")
	}
}
