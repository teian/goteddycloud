// SPDX-License-Identifier: GPL-2.0-only
package syncutil

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

func TestGateReaderSharingWriterPreferenceAndCancellation(t *testing.T) {
	var gate RWGate
	reader, err := gate.RLock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := gate.RLock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second()
	ctx, cancel := context.WithCancel(context.Background())
	writer := make(chan error, 1)
	go func() {
		release, err := gate.Lock(ctx)
		if release != nil {
			release()
		}
		writer <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		gate.mu.Lock()
		waiting := gate.waitingWriters
		gate.mu.Unlock()
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			reader()
			cancel()
			t.Fatal("writer did not wait")
		}
		runtime.Gosched()
	}
	waitingReader := make(chan struct{})
	go func() { release, _ := gate.RLock(context.Background()); release(); close(waitingReader) }()
	select {
	case <-waitingReader:
		reader()
		cancel()
		t.Fatal("reader bypassed waiting writer")
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	if err := <-writer; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// Cancelling the writer must wake readers even while an older reader exists.
	select {
	case <-waitingReader:
	case <-time.After(time.Second):
		reader()
		t.Fatal("writer cancellation did not release reader preference")
	}
	reader()
	release, err := gate.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if release, err := gate.Lock(ctx); !errors.Is(err, context.Canceled) {
		if release != nil {
			release()
		}
		t.Fatal(err)
	}
}
