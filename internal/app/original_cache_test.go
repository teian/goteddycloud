// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"goteddycloud/internal/certificates"
	"goteddycloud/internal/config"
	"goteddycloud/internal/content"
	serverruntime "goteddycloud/internal/runtime"
)

type originalCacheCloud struct {
	mu                sync.Mutex
	claims, downloads int
	data              []byte
	started           chan struct{}
	release           chan struct{}
	once              sync.Once
}

func (c *originalCacheCloud) Do(ctx context.Context, _, _ config.Snapshot, _, path, _ string, headers http.Header, _ []byte) (*http.Response, error) {
	c.mu.Lock()
	claim := strings.HasPrefix(path, "/v1/claim/")
	if claim {
		c.claims++
	} else {
		c.downloads++
	}
	c.mu.Unlock()
	status, data := 401, []byte(nil)
	if !claim {
		c.once.Do(func() { close(c.started) })
		select {
		case <-c.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		status, data = 200, c.data
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), ContentLength: int64(len(data)), Body: io.NopCloser(bytes.NewReader(data))}, nil
}
func (c *originalCacheCloud) counts() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.claims, c.downloads
}

func TestOriginalCacheAuthenticatedClaim401BackfillsBoxLibrary(t *testing.T) {
	s := fixtureApp(t)
	const id, ruid = "001122334455", "97763c1a500304e0"
	for _, key := range []string{"cloud.enabled", "cloud.cacheContent", "cloud.cacheToLibrary"} {
		if err := s.Settings.Set("", key, "true"); err != nil {
			t.Fatal(err)
		}
	}
	for key, value := range map[string]string{"core.contentdir": "original-background", "core.librarydir": "original-background-library"} {
		if err := s.Settings.Set(id, key, value); err != nil {
			t.Fatal(err)
		}
	}
	snap := s.Settings.Snapshot(id)
	for _, key := range []string{"internal.contentdirfull", "internal.librarydirfull"} {
		if err := os.MkdirAll(snap.String(key), 0700); err != nil {
			t.Fatal(err)
		}
	}
	data := syntheticTAF(t)
	mock := &originalCacheCloud{data: data, started: make(chan struct{}), release: make(chan struct{})}
	s.Cloud = mock
	global := s.Settings.Snapshot("")
	t.Setenv("GOTEDDY_BOX_AUTH_WORKER", "1")
	tb1, err := certificates.Load(global, "core.server_cert")
	if err != nil {
		t.Fatal(err)
	}
	tb2, err := certificates.Load(global, "core.server_cert_tb2")
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.LoadX509KeyPair(filepath.Join(snap.Base, "certs/client", id, "teddy-cert.pem"), filepath.Join(snap.Base, "certs/client", id, "teddy-key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	cert, err := x509.ParseCertificate(tb1.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots.AddCert(cert)
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan map[string]string, 1)
	done := make(chan error, 1)
	go func() {
		done <- serverruntime.Run(ctx, serverruntime.Options{
			Box: "127.0.0.1:0", TB1: tb1, TB2: tb2, VerifyBoxCertificate: s.Identity.VerifyCertificate,
			Management: http.HandlerFunc(s.Management), Device: http.HandlerFunc(s.Device),
			WorkerCommand: []string{os.Args[0], "-test.run=^TestBoxAuthWorker$"}, Stderr: os.Stderr, Ready: func(v map[string]string) { ready <- v },
		})
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
	case <-time.After(10 * time.Second):
		t.Fatal("box listener not ready")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	claim := func(uid, password string, want int) {
		t.Helper()
		r, _ := http.NewRequest("GET", "https://"+address+"/v1/claim/"+uid, nil)
		r.Header.Set("Authorization", "BD "+password)
		r.Header.Set("User-Agent", "TB/synthetic-original-cache")
		res, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != want {
			t.Fatal("claim response changed", res.StatusCode)
		}
	}
	claim(ruid, strings.Repeat("AB", 32), 401)
	select {
	case <-mock.started:
	case <-time.After(5 * time.Second):
		t.Fatal("claim did not trigger background content download")
	}
	// The content transfer is blocked, but another claim must still complete.
	claim(ruid, strings.Repeat("AB", 32), 401)
	if claims, downloads := mock.counts(); claims != 2 || downloads != 1 {
		t.Fatal("duplicate background download", claims, downloads)
	}
	close(mock.release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		m, err := s.Content.Metadata(snap, ruid)
		if err == nil && m.String("source") == "lib://by/audioID/123456.taf" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background cache did not publish library file", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	stored, err := os.ReadFile(filepath.Join(snap.String("internal.librarydirfull"), "by/audioID/123456.taf"))
	if err != nil || !bytes.Equal(stored, data) {
		t.Fatal("box library audio differs", err)
	}
	if _, err := os.Stat(filepath.Join(global.String("internal.librarydirfull"), "by/audioID/123456.taf")); !os.IsNotExist(err) {
		t.Fatal("background cache wrote to global library", err)
	}
	// The manual path reuses the same durable file without another cloud request.
	response := perform(s, "GET", "/content/download/97763C1A/500304E0?overlay="+id, nil, false)
	if response.Code != 200 || !bytes.Equal(response.Body.Bytes(), data) {
		t.Fatal("manual download cannot use background cache", response.Code)
	}
	const assigned = "01000000500304e0"
	if err := s.Content.Update(snap, assigned, func(m content.Metadata) error {
		m.Set("source", "lib://assigned.taf")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snap.String("internal.librarydirfull"), "assigned.taf"), data, 0600); err != nil {
		t.Fatal(err)
	}
	claim(assigned, strings.Repeat("AB", 32), 401)
	metadata, err := s.Content.Metadata(snap, assigned)
	if err != nil || metadata.String("source") != "lib://assigned.taf" {
		t.Fatal("original cache replaced a local assignment", err)
	}
	for _, custom := range []struct{ uid, password string }{
		{"7c47c80e500304e0", strings.Repeat("00", 32)},
		{"0102030405060708", strings.Repeat("AB", 32)},
	} {
		claim(custom.uid, custom.password, 200)
	}
	if claims, downloads := mock.counts(); claims != 3 || downloads != 1 {
		t.Fatal("custom tag forwarded", claims, downloads)
	}
}

func TestOriginalCacheShutdownCancelsWorkAndRejectsNewJobs(t *testing.T) {
	s := fixtureApp(t)
	const id, ruid = "001122334455", "97763c1a500304e0"
	for _, key := range []string{"cloud.enabled", "cloud.cacheContent"} {
		if err := s.Settings.Set("", key, "true"); err != nil {
			t.Fatal(err)
		}
	}
	snap := s.Settings.Snapshot(id)
	if err := s.Content.Update(snap, ruid, func(m content.Metadata) error { m.Set("cloud_auth", strings.Repeat("AB", 32)); return nil }); err != nil {
		t.Fatal(err)
	}
	m, _ := s.Content.Metadata(snap, ruid)
	mock := &originalCacheCloud{data: syntheticTAF(t), started: make(chan struct{}), release: make(chan struct{})}
	s.Cloud = mock
	s.scheduleOriginalCache(id, ruid, m, "")
	select {
	case <-mock.started:
	case <-time.After(5 * time.Second):
		t.Fatal("background work not started")
	}
	done := make(chan struct{})
	go func() { s.stopOriginalCache(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("background work did not stop")
	}
	s.scheduleOriginalCache(id, ruid, m, "")
	if _, downloads := mock.counts(); downloads != 1 {
		t.Fatal("background work restarted after shutdown")
	}
}

func TestOriginalCacheRechecksDisabledBoxBeforeFetching(t *testing.T) {
	s := fixtureApp(t)
	const id, ruid = "001122334455", "97763c1a500304e0"
	for _, key := range []string{"cloud.enabled", "cloud.cacheContent"} {
		if err := s.Settings.Set("", key, "true"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Content.Update(s.Settings.Snapshot(id), ruid, func(m content.Metadata) error { m.Set("cloud_auth", strings.Repeat("AB", 32)); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.Settings.Set(id, "toniebox.api_access", "false"); err != nil {
		t.Fatal(err)
	}
	mock := &originalCacheCloud{started: make(chan struct{}), release: make(chan struct{})}
	s.Cloud = mock
	if !s.cacheOriginal(context.Background(), id, ruid, "") {
		t.Fatal("disabled box should skip background work")
	}
	if _, downloads := mock.counts(); downloads != 0 {
		t.Fatal("disabled box fetched audio")
	}
}
