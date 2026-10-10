// SPDX-License-Identifier: GPL-2.0-only
package content

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"goteddycloud/internal/config"
	"goteddycloud/internal/storage"
)

func TestPrivateAreaStreamNetworkPolicy(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); _, _ = w.Write([]byte("audio")) }))
	defer server.Close()
	base := t.TempDir()
	backend, err := storage.NewScoped(storage.Filesystem{}, base)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	settings, err := config.OpenWithStorage(base, backend, false)
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	if stream, err := remoteStream(server.URL, settings.Snapshot(""))(context.Background()); err == nil {
		stream.Close()
		t.Fatal("private address allowed by default")
	}
	if calls.Load() != 0 {
		t.Fatal("private server contacted before rejection")
	}
	if err := settings.Set("", "core.allowPrivateFetch", "true"); err != nil {
		t.Fatal(err)
	}
	stream, err := remoteStream(server.URL, settings.Snapshot(""))(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil || string(data) != "audio" {
		t.Fatalf("explicit private access: %s %v", data, err)
	}
}
