// SPDX-License-Identifier: GPL-2.0-only
// Package teststorage supplies isolated S3 backends for tests only.
package teststorage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"goteddycloud/internal/storage"
)

func New(t testing.TB, base string, middleware func(http.Handler) http.Handler) *storage.S3 {
	t.Helper()
	if backend := external(t, base, middleware != nil); backend != nil {
		return backend
	}
	t.Setenv("AWS_ACCESS_KEY_ID", "synthetic-test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "synthetic-test-secret")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	emulator := gofakes3.New(s3mem.New()).Server()
	// The pinned emulator writes ranged content with HTTP 200; normalize it to
	// the S3 protocol's HTTP 206 when Content-Range is present.
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { emulator.ServeHTTP(&rangeStatus{ResponseWriter: w}, r) })
	if middleware != nil {
		handler = middleware(handler)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	backend, err := storage.NewS3(context.Background(), base, storage.S3Options{Bucket: "test-deployment", Prefix: "deployment", Region: "us-east-1", Endpoint: server.URL, PathStyle: true, TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	// Bucket provisioning is intentionally a separate deployment responsibility.
	req, err := http.NewRequest(http.MethodPut, server.URL+"/test-deployment", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("bucket creation", response.StatusCode)
	}
	if err = backend.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	return backend
}

type rangeStatus struct {
	http.ResponseWriter
	wrote bool
}

func (w *rangeStatus) WriteHeader(status int) {
	if status == 200 && w.Header().Get("Content-Range") != "" {
		status = 206
	}
	w.wrote = true
	w.ResponseWriter.WriteHeader(status)
}
func (w *rangeStatus) Write(p []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(p)
}
