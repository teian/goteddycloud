// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"goteddycloud/internal/config"
	"goteddycloud/internal/content"
	"goteddycloud/internal/taf"
)

type contentCloud struct {
	data   []byte
	length int64
	calls  atomic.Int32
}

func (c *contentCloud) Do(_ context.Context, _ config.Snapshot, _ config.Snapshot, _, _, _ string, headers http.Header, _ []byte) (*http.Response, error) {
	c.calls.Add(1)
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/octet-stream"}}, ContentLength: c.length, Body: io.NopCloser(bytes.NewReader(c.data))}, nil
}
func syntheticTAF(t *testing.T, count ...int) []byte {
	t.Helper()
	file, err := os.Create(filepath.Join(t.TempDir(), "synthetic.taf"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer, err := taf.NewWriter(file, 123456)
	if err != nil {
		t.Fatal(err)
	}
	head := []byte{'O', 'p', 'u', 's', 'H', 'e', 'a', 'd', 1, 2, 0x38, 1, 0x80, 0xbb, 0, 0, 0, 0, 0}
	if err := writer.StartTrack(head); err != nil {
		t.Fatal(err)
	}
	packets := 1
	if len(count) > 0 {
		packets = count[0]
	}
	for range packets {
		if err := writer.Packet([]byte{0xfb, 3, 0xff, 0xfe, 0xff, 0xfe, 0xff, 0xfe}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func TestCloudConcurrentMissesPublishOnceAndMigrate(t *testing.T) {
	s := fixtureApp(t)
	for _, key := range []string{"cloud.enabled", "cloud.enableV2Content", "cloud.cacheContent", "cloud.cacheToLibrary"} {
		if err := s.Settings.Set("", key, "true"); err != nil {
			t.Fatal(err)
		}
	}
	data := syntheticTAF(t)
	mock := &contentCloud{data: data, length: int64(len(data))}
	s.Cloud = mock
	const ruid = "0200000002000000"
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			reply := perform(s, "GET", "/v1/content/"+ruid, nil, true)
			if reply.Code != 200 || !bytes.Equal(reply.Body.Bytes(), data) {
				t.Errorf("reply %d bytes %d", reply.Code, reply.Body.Len())
			}
		})
	}
	group.Wait()
	if mock.calls.Load() != 1 {
		t.Fatalf("duplicate misses: %d", mock.calls.Load())
	}
	snap := s.Settings.Snapshot("")
	metadata, err := s.Content.Metadata(snap, ruid)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.String("source") != "lib://by/audioID/123456.taf" {
		t.Fatal(metadata)
	}
	path := filepath.Join(snap.String("internal.librarydirfull"), "by/audioID/123456.taf")
	cached, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(cached, data) {
		t.Fatal("cache differs", err)
	}
	uidPath, _ := content.UIDPath(ruid)
	if _, err := os.Stat(filepath.Join(snap.String("internal.contentdirfull"), uidPath)); !os.IsNotExist(err) {
		t.Fatal("UID duplicate retained", err)
	}
}
func TestInterruptedCloudRefreshPreservesValidCache(t *testing.T) {
	s := fixtureApp(t)
	for _, key := range []string{"cloud.enabled", "cloud.enableV2Content", "cloud.cacheContent"} {
		if err := s.Settings.Set("", key, "true"); err != nil {
			t.Fatal(err)
		}
	}
	data := syntheticTAF(t)
	snap := s.Settings.Snapshot("")
	const ruid = "0200000002000000"
	cache, err := s.Content.BeginCache(snap, ruid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := cache.Commit(context.Background(), int64(len(data))); err != nil {
		t.Fatal(err)
	}
	cache.Close()
	uid, _ := content.UIDFromRUID(ruid)
	s.freshness[""] = []uint64{uid}
	s.Cloud = &contentCloud{data: data[:len(data)-5], length: int64(len(data))}
	perform(s, "GET", "/v1/content/"+ruid, nil, true)
	file, _, err := s.Content.Open(snap, snap, ruid)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	retained, _ := io.ReadAll(file)
	if !bytes.Equal(retained, data) {
		t.Fatal("valid cache replaced")
	}
	leftovers, _ := filepath.Glob(filepath.Join(snap.String("internal.contentdirfull"), "*", ".cache-*"))
	if len(leftovers) > 0 {
		t.Fatal(leftovers)
	}
}

func TestCloudLibraryCollisionAndCancelledPublication(t *testing.T) {
	s := fixtureApp(t)
	for _, key := range []string{"cloud.enabled", "cloud.enableV2Content", "cloud.cacheContent", "cloud.cacheToLibrary"} {
		if err := s.Settings.Set("", key, "true"); err != nil {
			t.Fatal(err)
		}
	}
	const ruid = "0200000002000000"
	first, second := syntheticTAF(t), syntheticTAF(t, 2)
	if bytes.Equal(first, second) {
		t.Fatal("fixture must differ")
	}
	s.Cloud = &contentCloud{data: first, length: int64(len(first))}
	perform(s, "GET", "/v1/content/"+ruid, nil, true)
	uid, _ := content.UIDFromRUID(ruid)
	s.freshness[""] = []uint64{uid}
	s.Cloud = &contentCloud{data: second, length: int64(len(second))}
	perform(s, "GET", "/v1/content/"+ruid, nil, true)
	snap := s.Settings.Snapshot("")
	library, err := os.ReadFile(filepath.Join(snap.String("internal.librarydirfull"), "by/audioID/123456.taf"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(library, first) {
		t.Fatal("library collision replaced original")
	}
	file, _, err := s.Content.Open(snap, snap, ruid)
	if err != nil {
		t.Fatal(err)
	}
	current, _ := io.ReadAll(file)
	file.Close()
	if !bytes.Equal(current, second) {
		t.Fatal("collision lost valid UID cache")
	}
	cache, err := s.Content.BeginCache(snap, ruid)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	if _, err := cache.Write(first); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := cache.Commit(ctx, int64(len(first))); err != context.Canceled {
		t.Fatal(err)
	}
	file, _, err = s.Content.Open(snap, snap, ruid)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	current, _ = io.ReadAll(file)
	if !bytes.Equal(current, second) {
		t.Fatal("cancelled cache published")
	}
}
