// SPDX-License-Identifier: GPL-2.0-only
package certificates

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"os"
	"path/filepath"
	"testing"

	"goteddycloud/internal/config"
)

func TestClientGenerationAndPreservation(t *testing.T) {
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, tb2 := range []bool{false, true} {
		snap := store.Snapshot("")
		prefix := "core.server_cert"
		if tb2 {
			prefix += "_tb2"
		}
		if _, err := Ensure(snap, prefix); err != nil {
			t.Fatal(err)
		}
		destination := t.TempDir()
		paths, err := GenerateClient(snap, "aabbccddeeff", destination, tb2)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(paths["crt"])
		crt, err := Certificate(data)
		if err != nil {
			t.Fatal(err)
		}
		if tb2 {
			if crt.Subject.CommonName != "AABBCCDDEEFF" {
				t.Fatal(crt.Subject)
			}
			if _, ok := crt.PublicKey.(*ecdsa.PublicKey); !ok {
				t.Fatal("TB2 key type")
			}
		} else {
			if crt.Subject.CommonName != "b'aabbccddeeff'" {
				t.Fatal(crt.Subject)
			}
			if _, ok := crt.PublicKey.(*rsa.PublicKey); !ok {
				t.Fatal("TB1 key type")
			}
		}
		caData, err := os.ReadFile(certificatePath(snap, prefix, "ca"))
		if err != nil {
			t.Fatal(err)
		}
		ca, err := Certificate(caData)
		if err != nil {
			t.Fatal(err)
		}
		if err := crt.CheckSignatureFrom(ca); err != nil {
			t.Fatal(err)
		}
		if _, err := GenerateClient(snap, "aabbccddeeff", destination, tb2); err != nil {
			t.Fatal(err)
		}
		retained, _ := os.ReadFile(paths["crt"])
		if string(data) != string(retained) {
			t.Fatal("identity replaced")
		}
		if _, err := GenerateClient(snap, "001122334455", destination, tb2); err == nil {
			t.Fatal("wrong CN accepted")
		}
		if tb2 {
			os.WriteFile(paths["ca"], []byte("invalid"), 0600)
			if _, err := GenerateClient(snap, "aabbccddeeff", destination, tb2); err == nil {
				t.Fatal("invalid CA accepted")
			}
		}
		os.Remove(paths["key"])
		if _, err := GenerateClient(snap, "aabbccddeeff", destination, tb2); err == nil {
			t.Fatal("partial identity overwritten")
		}
		if _, err := os.Stat(filepath.Join(destination, filepath.Base(paths["crt"]))); err != nil {
			t.Fatal(err)
		}
	}
}
