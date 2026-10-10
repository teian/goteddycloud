// SPDX-License-Identifier: GPL-2.0-only
package certificates

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"golang.org/x/crypto/acme"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"golang.org/x/crypto/acme/autocert"
	"goteddycloud/internal/config"
	"goteddycloud/internal/storage"
	"goteddycloud/internal/teststorage"
)

func TestManagementManualCertificates(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(map[bool]string{false: "filesystem", true: "s3"}[remote], func(t *testing.T) {
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
			// Independent certificate families prove that management need not present the box pair.
			box, err := Ensure(store.Snapshot(""), "core.server_cert")
			if err != nil {
				t.Fatal(err)
			}
			manual, err := Ensure(store.Snapshot(""), "core.server_cert_tb2")
			if err != nil {
				t.Fatal(err)
			}
			for key, value := range map[string]string{
				"core.httpsecure.certificate_file": store.String("", "core.server_cert_tb2.file.crt"),
				"core.httpsecure.key_file":         store.String("", "core.server_cert_tb2.file.key"),
			} {
				if err := store.Set("", key, value); err != nil {
					t.Fatal(err)
				}
			}
			web, err := ManagementTLS(context.Background(), store.Snapshot(""))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(web.Config.Certificates[0].Certificate[0], manual.Certificate[0]) || bytes.Equal(box.Certificate[0], manual.Certificate[0]) || web.HTTPHandler != nil {
				t.Fatal("wrong management certificate")
			}
			if err := store.Set("", "core.httpsecure.key_file", ""); err != nil {
				t.Fatal(err)
			}
			if _, err := ManagementTLS(context.Background(), store.Snapshot("")); err == nil {
				t.Fatal("accepted incomplete key pair")
			}
		})
	}
}

func TestACMEConfigurationAndDomainPolicy(t *testing.T) {
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	set := func(k, v string) {
		t.Helper()
		if err := store.Set("", "core.httpsecure.acme."+k, v); err != nil {
			t.Fatal(err)
		}
	}
	set("enabled", "true")
	if _, err := ManagementTLS(context.Background(), store.Snapshot("")); err == nil {
		t.Fatal("missing terms accepted")
	}
	set("accept_terms", "true")
	if _, err := ManagementTLS(context.Background(), store.Snapshot("")); err == nil {
		t.Fatal("missing domains accepted")
	}
	for _, domain := range []string{"localhost", "127.0.0.1", "*.example.net", "example.net:443", "-bad.example.net", "bad..example.net", "UPPER.example.net"} {
		set("domains", domain)
		if _, err := ManagementTLS(context.Background(), store.Snapshot("")); err == nil {
			t.Fatal("accepted invalid domain", domain)
		}
	}
	set("domains", "teddy.example.net xn--bcher-kva.example.net")
	set("directory_url", "http://ca.invalid/directory")
	if _, err := ManagementTLS(context.Background(), store.Snapshot("")); err == nil {
		t.Fatal("accepted insecure ACME directory")
	}
	set("directory_url", "https://ca.invalid/directory")
	if err := ValidateManagementTLS(store.Snapshot("")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(store.Snapshot("").Base, "certs", "acme")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("validation created cache", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	web, err := ManagementTLS(ctx, store.Snapshot(""))
	if err != nil {
		t.Fatal(err)
	}
	if web.Config.MinVersion != tls.VersionTLS12 || web.HTTPHandler == nil || !slices.Contains(web.Config.NextProtos, acme.ALPNProto) || slices.Contains(web.Config.NextProtos, "h2") {
		t.Fatal("missing ACME config")
	}
	if _, err := web.Config.GetCertificate(&tls.ClientHelloInfo{ServerName: "unlisted.example.net"}); err == nil {
		t.Fatal("allowed unlisted name")
	}
	// Unknown HTTP challenge must not leak into management, ordinary requests still do.
	fallback := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201) })
	for path, status := range map[string]int{"/api/auth/status": 201, "/.well-known/acme-challenge/missing": 404} {
		r := httptest.NewRequest("GET", "http://teddy.example.net"+path, nil)
		w := httptest.NewRecorder()
		web.HTTPHandler(fallback).ServeHTTP(w, r)
		if w.Code != status {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	cancel()
	if _, err := web.Config.GetCertificate(&tls.ClientHelloInfo{ServerName: "teddy.example.net"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestACMECacheDurabilityAndConfinement(t *testing.T) {
	for _, remote := range []bool{false, true} {
		t.Run(map[bool]string{false: "filesystem", true: "s3"}[remote], func(t *testing.T) {
			base := t.TempDir()
			var backend storage.Backend = storage.Filesystem{}
			if remote {
				backend = teststorage.New(t, base, nil)
			}
			directory := filepath.Join(base, "acme")
			if err := backend.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cache := &acmeCache{backend: backend, directory: directory, lifetime: ctx}
			key := "../../account/key"
			if _, err := cache.Get(ctx, key); !errors.Is(err, autocert.ErrCacheMiss) {
				t.Fatal(err)
			}
			data := []byte("private account and certificate material")
			if err := cache.Put(ctx, key, data); err != nil {
				t.Fatal(err)
			}
			reopened := &acmeCache{backend: backend, directory: directory, lifetime: ctx}
			got, err := reopened.Get(ctx, key)
			if err != nil || !bytes.Equal(got, data) {
				t.Fatal(string(got), err)
			}
			root, name, err := cache.root(ctx, key)
			if err != nil {
				t.Fatal(err)
			}
			info, err := root.Stat(name)
			root.Close()
			if err != nil || !remote && info.Mode().Perm() != 0600 {
				t.Fatal(info, err)
			}
			if err := cache.Put(ctx, key, make([]byte, MaxMaterial+1)); err == nil {
				t.Fatal("oversized cache accepted")
			}
			if err := cache.Delete(ctx, key); err != nil {
				t.Fatal(err)
			}
			if err := cache.Delete(ctx, key); err != nil {
				t.Fatal("delete missing entry", err)
			}
			cancel()
			if err := cache.Put(context.Background(), key, data); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if _, err := backend.Stat(filepath.Join(base, "account", "key")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("cache escaped root", err)
			}
		})
	}
}

type testRoundTripper func(*http.Request) (*http.Response, error)

func (f testRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestACMENetworkCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	transport := lifetimeTransport{ctx: ctx, base: testRoundTripper(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	done := make(chan error, 1)
	go func() {
		r, _ := http.NewRequest("GET", "https://ca.invalid/directory", nil)
		_, err := transport.RoundTrip(r)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("request survived shutdown")
	}
	r, _ := http.NewRequest("GET", "https://ca.invalid/directory", nil)
	if _, err := transport.RoundTrip(r); !errors.Is(err, context.Canceled) {
		t.Fatal("network request after shutdown", err)
	}
}
