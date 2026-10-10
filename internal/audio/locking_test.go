// SPDX-License-Identifier: GPL-2.0-only
package audio

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"goteddycloud/internal/taf"
)

type blockedCopyWriter struct {
	entered, release chan struct{}
	once             sync.Once
}

func (w *blockedCopyWriter) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(data), nil
}

func TestFinalCopyDoesNotBlockStateReadersOrClose(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "spool-*.taf")
	if err != nil {
		t.Fatal(err)
	}
	writer, err := taf.NewWriter(file, 1234)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.StartTrack([]byte{'O', 'p', 'u', 's', 'H', 'e', 'a', 'd', 1, 2, 0x38, 1, 0x80, 0xbb, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Packet([]byte{0xfb, 3, 0xff, 0xfe, 0xff, 0xfe, 0xff, 0xfe}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	stream := &Stream{file: file, size: info.Size(), done: make(chan struct{}), cancel: func() {}}
	close(stream.done)
	destination := &blockedCopyWriter{entered: make(chan struct{}), release: make(chan struct{})}
	copied := make(chan error, 1)
	go func() { copied <- stream.FinalCopy(destination) }()
	release := sync.OnceFunc(func() { close(destination.release) })
	defer func() { release(); <-copied; stream.Close() }()
	select {
	case <-destination.entered:
	case <-time.After(time.Second):
		t.Fatal("copy did not start")
	}
	read := make(chan error, 1)
	go func() { _, err := stream.ReadAtContext(context.Background(), make([]byte, 4096), 0); read <- err }()
	select {
	case err := <-read:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("copy held spool lock")
	}
	closed := make(chan error, 1)
	go func() { closed <- stream.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("slow destination blocked close")
	}
	if err := stream.Close(); err != nil {
		t.Fatal("close was not idempotent", err)
	}
}

func TestCancelledDestinationNeverAcquiresReadyToken(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 100 {
		release, err := lockDestination(ctx, t.TempDir()+"/output.taf")
		if release != nil {
			release()
		}
		if err == nil {
			t.Fatal("cancelled job acquired destination")
		}
	}
}
