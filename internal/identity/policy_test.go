// SPDX-License-Identifier: GPL-2.0-only
package identity

import (
	"testing"
	"time"

	"goteddycloud/internal/config"
	"goteddycloud/internal/runtime"
)

func TestLegacyRegistrationAndAccess(t *testing.T) {
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p := &Policy{Store: store}
	if err := store.Set("", "core.allowNewBox", "false"); err != nil {
		t.Fatal(err)
	}
	peer := runtime.Peer{Box: true, CommonName: "b'001122334455'", Issuer: "CN=Boxine Factory SubCA"}
	if id, ok, err := p.Resolve(peer); id != "" || ok || err != nil {
		t.Fatal(id, ok, err)
	}
	if err := store.Set("", "core.allowNewBox", "true"); err != nil {
		t.Fatal(err)
	}
	id, ok, err := p.Resolve(peer)
	if err != nil || !ok || id != "001122334455" {
		t.Fatal(id, ok, err)
	}
	if err := store.Set(id, "toniebox.api_access", "false"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := p.Resolve(peer); ok || err != nil {
		t.Fatal(ok, err)
	}
	peer.Box = false
	if id, ok, err := p.Resolve(peer); id != "" || ok || err != nil {
		t.Fatal("web identity accepted")
	}
	peer.Box = true
	peer.Issuer = "unrelated"
	if id, ok, err := p.Resolve(peer); id != "" || ok || err != nil {
		t.Fatal("unrecognized issuer accepted")
	}
}

func TestKnownBoxDoesNotWaitForRegistration(t *testing.T) {
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Set("001122334455", "toniebox.api_access", "true"); err != nil {
		t.Fatal(err)
	}
	policy := &Policy{Store: store}
	policy.mu.Lock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		id, ok, err := policy.Resolve(runtime.Peer{Box: true, CommonName: "001122334455", Subject: "TeddyCloud"})
		if id != "001122334455" || !ok || err != nil {
			t.Error(id, ok, err)
		}
	}()
	select {
	case <-done:
		policy.mu.Unlock()
	case <-time.After(time.Second):
		policy.mu.Unlock()
		<-done
		t.Fatal("known device waited for registration")
	}
}
