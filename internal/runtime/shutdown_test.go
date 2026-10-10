// SPDX-License-Identifier: GPL-2.0-only
package runtime

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRunDrainsCancelledHandlerBeforeReturning(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started, cleaning, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(cleaning)
		<-release
		io.WriteString(w, "finished")
		close(finished)
	})
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{HTTP: "127.0.0.1:0", Management: handler, Device: handler, Ready: func(a map[string]string) { ready <- a["http"] }})
	}()
	var address string
	select {
	case address = <-ready:
	case err := <-done:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("startup timeout")
	}
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		response, err := http.Get("http://" + address)
		if err == nil {
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}
	}()
	defer func() { close(release); <-clientDone }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler not started")
	}
	cancel()
	select {
	case <-cleaning:
	case <-time.After(time.Second):
		t.Fatal("handler context not cancelled")
	}
	select {
	case err := <-done:
		t.Fatalf("returned before handler cleanup: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	// Release explicitly, leaving the deferred release safe on earlier failures.
	release <- struct{}{}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish")
	}
	select {
	case <-finished:
	default:
		t.Fatal("handler still running after shutdown")
	}
}

func TestShutdownDeadlineClosesConnections(t *testing.T) {
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(started); <-release; close(finished) }))
	defer server.Close()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		response, err := server.Client().Get(server.URL)
		if err == nil {
			response.Body.Close()
		}
	}()
	defer func() { close(release); <-finished; <-clientDone }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	began := time.Now()
	if err := shutdownServers(ctx, []*http.Server{server.Config}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("missing shutdown deadline error: %v", err)
	}
	if time.Since(began) > time.Second {
		t.Fatal("shutdown deadline was not bounded")
	}
	select {
	case <-clientDone:
	case <-time.After(time.Second):
		t.Fatal("connection remained open after deadline")
	}
}
