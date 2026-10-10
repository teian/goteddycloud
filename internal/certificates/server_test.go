// SPDX-License-Identifier: GPL-2.0-only
package certificates

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"

	"goteddycloud/internal/config"
)

func TestGeneratedPairsArePreservedAndVerifiable(t *testing.T) {
	base := t.TempDir()
	store, err := config.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, prefix := range []string{"core.server_cert", "core.server_cert_tb2"} {
		snap := store.Snapshot("")
		pair, err := Ensure(snap, prefix)
		if err != nil {
			t.Fatal(err)
		}
		if prefix == "core.server_cert" {
			if _, ok := pair.PrivateKey.(*rsa.PrivateKey); !ok {
				t.Fatal("TB1 needs RSA")
			}
		} else {
			if _, ok := pair.PrivateKey.(*ecdsa.PrivateKey); !ok {
				t.Fatal("TB2 needs ECDSA")
			}
		}
		leaf, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			t.Fatal(err)
		}
		ca, err := x509.ParseCertificate(pair.Certificate[1])
		if err != nil {
			t.Fatal(err)
		}
		roots := x509.NewCertPool()
		roots.AddCert(ca)
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, DNSName: "localhost"}); err != nil {
			t.Fatal(err)
		}
		keyPath := filepath.Join(base, snap.String(prefix+".file.key"))
		before, err := os.ReadFile(keyPath)
		if err != nil {
			t.Fatal(err)
		}
		second, err := Ensure(snap, prefix)
		if err != nil {
			t.Fatal(err)
		}
		after, _ := os.ReadFile(keyPath)
		if string(before) != string(after) || string(pair.Certificate[0]) != string(second.Certificate[0]) {
			t.Fatal("existing identity replaced")
		}
		if err := os.WriteFile(keyPath, []byte("broken imported key"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Ensure(snap, prefix); err == nil {
			t.Fatal("invalid existing material was regenerated")
		}
		retained, _ := os.ReadFile(keyPath)
		if string(retained) != "broken imported key" {
			t.Fatal("invalid import overwritten")
		}
	}
}
func TestDisabledGenerationAndIncompleteInlineMaterial(t *testing.T) {
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Set("", "internal.autogen_certs", "false"); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(store.Snapshot(""), "core.server_cert"); err == nil {
		t.Fatal("generation disabled")
	}
	snap := store.Snapshot("")
	snap.Values["core.server_cert.data.crt"] = "incomplete"
	if _, err := Ensure(snap, "core.server_cert"); err == nil {
		t.Fatal("incomplete inline pair accepted")
	}
}
