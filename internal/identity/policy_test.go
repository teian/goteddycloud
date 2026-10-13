// SPDX-License-Identifier: GPL-2.0-only
package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"encoding/pem"
	"goteddycloud/internal/config"
	"goteddycloud/internal/runtime"
	"goteddycloud/internal/teststorage"
)

func certificate(t *testing.T, cn string, change func(*x509.Certificate)) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: cn, Organization: []string{"TeddyCloud"}}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	if change != nil {
		change(cert)
	}
	raw, err := x509.CreateCertificate(rand.Reader, cert, cert, key.Public(), key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func peer(t *testing.T, raw []byte) runtime.Peer {
	t.Helper()
	cert, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	return runtime.Peer{Box: true, CommonName: cert.Subject.CommonName, Subject: "TeddyCloud", Issuer: "Boxine Factory SubCA", CertificateSHA256: hex.EncodeToString(hash[:])}
}
func install(t *testing.T, store *config.Store, id string, raw []byte) string {
	t.Helper()
	path := filepath.Join(store.String("", "internal.certdirfull"), id, "client.der")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestOnlyImportedCertificatesCanRegister(t *testing.T) {
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p := &Policy{Store: store}
	raw := certificate(t, "b'001122334455'", nil)
	identity := peer(t, raw)
	// Names and allowNewBox must never turn an unknown certificate into trust.
	for _, legacyFlag := range []string{"true", "false"} {
		if err := store.Set("", "core.boxCertAuth", legacyFlag); err != nil {
			t.Fatal(err)
		}
		if _, ok, _ := p.Resolve(identity); ok {
			t.Fatal("unknown certificate accepted")
		}
		if p.VerifyCertificate(raw) == nil {
			t.Fatal("unknown TLS certificate accepted")
		}
	}
	if len(store.BoxIDs()) != 0 {
		t.Fatal("unauthorized registration")
	}
	install(t, store, "001122334455", raw)
	if err := p.VerifyCertificate(raw); err != nil {
		t.Fatal(err)
	}
	if len(store.BoxIDs()) != 0 {
		t.Fatal("handshake verification mutated settings before proof of possession")
	}
	if err := store.Set("", "core.allowNewBox", "false"); err != nil {
		t.Fatal(err)
	}
	if p.VerifyCertificate(raw) == nil {
		t.Fatal("unregistered box accepted with registration disabled")
	}
	if err := store.Set("", "core.allowNewBox", "true"); err != nil {
		t.Fatal(err)
	}
	id, ok, err := p.Resolve(identity)
	if err != nil || !ok || id != "001122334455" {
		t.Fatal(id, ok, err)
	}
	// An attacker can copy CN/issuer/subject, including the name of a known box.
	forged := certificate(t, "b'001122334455'", nil)
	if _, ok, _ := p.Resolve(peer(t, forged)); ok || p.VerifyCertificate(forged) == nil {
		t.Fatal("forged identity accepted")
	}
	identity.CertificateSHA256 = ""
	if _, ok, _ := p.Resolve(identity); ok {
		t.Fatal("name-only identity accepted")
	}
	identity = peer(t, raw)
	identity.Box = false
	if _, ok, _ := p.Resolve(identity); ok {
		t.Fatal("web identity accepted")
	}
	if err := store.Set(id, "toniebox.api_access", "false"); err != nil {
		t.Fatal(err)
	}
	if p.VerifyCertificate(raw) == nil {
		t.Fatal("disabled box accepted")
	}
	if _, ok, _ := p.Resolve(peer(t, raw)); ok {
		t.Fatal("existing connection not revoked")
	}
}
func TestPinSelectionRotationAndValidity(t *testing.T) {
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p := &Policy{Store: store}
	const id = "001122334455"
	original := certificate(t, id, nil)
	install(t, store, id, original)
	if _, ok, err := p.Resolve(peer(t, original)); !ok || err != nil {
		t.Fatal(ok, err)
	}
	replacement := certificate(t, id, nil)
	selected := filepath.Join(store.String("", "internal.certdirfull"), "selected.der")
	if err := os.WriteFile(selected, replacement, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(id, "core.client_cert.file.crt", selected); err != nil {
		t.Fatal(err)
	}
	if p.VerifyCertificate(original) == nil || p.VerifyCertificate(replacement) != nil {
		t.Fatal("explicit rotation did not replace old pin")
	}
	for _, tc := range []struct {
		name   string
		change func(*x509.Certificate)
	}{
		{"expired", func(c *x509.Certificate) { c.NotAfter = time.Now().Add(-time.Minute) }},
		{"future", func(c *x509.Certificate) { c.NotBefore = time.Now().Add(time.Minute) }},
		{"server only", func(c *x509.Certificate) { c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth} }},
		{"no signatures", func(c *x509.Certificate) { c.KeyUsage = x509.KeyUsageKeyEncipherment }},
		{"wrong CN", func(c *x509.Certificate) { c.Subject.CommonName = "AABBCCDDEEFF" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := certificate(t, id, tc.change)
			if err := os.WriteFile(selected, raw, 0600); err != nil {
				t.Fatal(err)
			}
			candidate := peer(t, raw)
			candidate.CommonName = id
			if _, ok, _ := p.Resolve(candidate); ok {
				t.Fatal("invalid selected certificate accepted")
			}
		})
	}
	if err := os.WriteFile(selected, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if p.VerifyCertificate(original) == nil {
		t.Fatal("corrupt selection fell back to original certificate")
	}
	if err := os.Remove(selected); err != nil {
		t.Fatal(err)
	}
	if p.VerifyCertificate(original) == nil {
		t.Fatal("missing selection fell back to original certificate")
	}
	// Explicitly imported fake/TB2 client identities are also exact pins.
	fake := certificate(t, id, nil)
	fakePath := filepath.Join(store.String("", "internal.certdirfull"), "fake.der")
	if err := os.WriteFile(fakePath, fake, 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(id, "core.client_cert_fake.file.crt", fakePath); err != nil {
		t.Fatal(err)
	}
	if p.VerifyCertificate(fake) != nil {
		t.Fatal("explicit fake client pin rejected")
	}
}

func TestCertificatePinsUseConfiguredStorage(t *testing.T) {
	base := t.TempDir()
	backend := teststorage.New(t, base, nil)
	store, err := config.OpenWithStorage(base, backend, false)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	dir := filepath.Join(store.String("", "internal.certdirfull"), "001122334455")
	if err := backend.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := backend.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	original := certificate(t, "001122334455", nil)
	if err := root.WriteAtomic("client.der", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: original}), 0600); err != nil {
		t.Fatal(err)
	}
	p := &Policy{Store: store}
	if _, ok, err := p.Resolve(peer(t, original)); !ok || err != nil {
		t.Fatal(ok, err)
	}
	replacement := certificate(t, "001122334455", nil)
	if err := root.WriteAtomic("client.der", replacement, 0600); err != nil {
		t.Fatal(err)
	}
	if p.VerifyCertificate(original) == nil || p.VerifyCertificate(replacement) != nil {
		t.Fatal("S3 pin rotation not observed")
	}
}

func TestMACSpellingCannotBypassKnownOverlayPolicy(t *testing.T) {
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	raw := certificate(t, "b'aabbccddeeff'", nil)
	path := install(t, store, "aabbccddeeff", raw)
	if err := store.Set("aabbccddeeff", "core.client_cert.file.crt", path); err != nil {
		t.Fatal(err)
	}
	p := &Policy{Store: store}
	candidate := peer(t, raw)
	id, ok, err := p.Resolve(candidate)
	if id != "aabbccddeeff" || !ok || err != nil {
		t.Fatal(id, ok, err)
	}
	if err := store.Set(id, "toniebox.api_access", "false"); err != nil {
		t.Fatal(err)
	}
	if p.VerifyCertificate(raw) == nil {
		t.Fatal("case variation bypassed disabled overlay")
	}
	if len(store.BoxIDs()) != 1 {
		t.Fatal("duplicate overlay registered")
	}
	if err := store.Set("AABBCCDDEEFF", "core.client_cert.file.crt", path); err != nil {
		t.Fatal(err)
	}
	if p.VerifyCertificate(raw) == nil {
		t.Fatal("ambiguous duplicate MAC overlays accepted")
	}
}
