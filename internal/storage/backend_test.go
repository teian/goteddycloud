// SPDX-License-Identifier: GPL-2.0-only
package storage_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"goteddycloud/internal/storage"
	"goteddycloud/internal/teststorage"
)

func TestBackendContract(t *testing.T) {
	for _, kind := range []string{"filesystem", "s3"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			var backend storage.Backend = storage.Filesystem{}
			if kind == "s3" {
				backend = teststorage.New(t, base, nil)
			}
			if err := backend.MkdirAll(filepath.Join(base, "nested", "empty"), 0700); err != nil {
				t.Fatal(err)
			}
			root, err := backend.OpenRoot(base)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if err = root.WriteAtomic("nested/empty", []byte("clobber"), 0600); err == nil {
				t.Fatal("overwrote directory")
			}
			payload := bytes.Repeat([]byte("abcdef"), 400000)
			if err = root.WriteAtomic("nested/audio", payload, 0600); err != nil {
				t.Fatal(err)
			}
			f, err := root.Open("nested/audio")
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			for _, offset := range []int64{0, 1048570, 2000000, 0} {
				buf := make([]byte, 32)
				if _, err = f.ReadAt(buf, offset); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(buf, payload[offset:offset+32]) {
					t.Fatal("wrong range")
				}
			}
			if _, err = f.Seek(-12, io.SeekEnd); err != nil {
				t.Fatal(err)
			}
			tail, err := io.ReadAll(f)
			if err != nil || !bytes.Equal(tail, payload[len(payload)-12:]) {
				t.Fatalf("tail: %q %v", tail, err)
			}
			if err = root.WriteReaderAtomic(context.Background(), "nested/audio", bytes.NewReader(payload), 0600, 4); err == nil {
				t.Fatal("overflow accepted")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err = root.WriteReaderAtomic(ctx, "nested/audio", bytes.NewReader(nil), 0600, 0); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			got, err := root.ReadFile("nested/audio")
			if err != nil || !bytes.Equal(got, payload) {
				t.Fatal("failed write changed destination", err)
			}
			dir, err := root.Open("nested")
			if err != nil {
				t.Fatal(err)
			}
			entries, err := dir.ReadDir(-1)
			dir.Close()
			if err != nil || len(entries) != 2 {
				t.Fatalf("listing: %v %v", entries, err)
			}
			if err = root.Remove("nested"); err == nil {
				t.Fatal("removed nonempty directory")
			}
			if err = root.Rename("nested/audio", "nested/renamed"); err != nil {
				t.Fatal(err)
			}
			if _, err = root.Open("nested/audio"); !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			appendFile, err := root.OpenFile("nested/renamed", os.O_WRONLY|os.O_APPEND, 0600)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = appendFile.Write([]byte("end")); err != nil {
				t.Fatal(err)
			}
			if err = appendFile.Sync(); err != nil {
				t.Fatal(err)
			}
			if err = appendFile.Close(); err != nil {
				t.Fatal(err)
			}
			got, err = root.ReadFile("nested/renamed")
			if err != nil || !bytes.Equal(got, append(payload, 'e', 'n', 'd')) {
				t.Fatal("append", err)
			}
			if _, err = root.OpenFile("nested/renamed", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600); !errors.Is(err, os.ErrExist) {
				t.Fatal(err)
			}
			if _, err = root.Open("../escape"); err == nil {
				t.Fatal("escaped root")
			}
			if err = root.Remove("nested/empty"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestS3ImmutableReadsAndPublicationFailure(t *testing.T) {
	base := t.TempDir()
	var fail atomic.Bool
	var ranged atomic.Int64
	backend := teststorage.New(t, base, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "GET" && r.Header.Get("Range") != "" {
				ranged.Add(1)
				if r.Header.Get("If-Match") == "" {
					t.Error("range read without object identity")
				}
			}
			if fail.Load() && r.Method == "PUT" {
				http.Error(w, "injected failure", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	root, err := backend.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	old := bytes.Repeat([]byte("a"), 2<<20)
	if err = root.WriteAtomic("audio", old, 0600); err != nil {
		t.Fatal(err)
	}
	f, err := root.Open("audio")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	block := make([]byte, 4096)
	for i := 0; i < 128; i++ {
		if _, err = f.Read(block); err != nil {
			t.Fatal(err)
		}
	}
	if ranged.Load() != 1 {
		t.Fatalf("read ahead used %d requests", ranged.Load())
	}
	if err = root.WriteAtomic("audio", bytes.Repeat([]byte("b"), len(old)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = f.ReadAt(block, 1<<20); err == nil {
		t.Fatal("mixed object versions")
	}
	fail.Store(true)
	if err = root.WriteAtomic("audio", []byte("broken"), 0600); err == nil {
		t.Fatal("failed upload accepted")
	}
	fail.Store(false)
	data, err := root.ReadFile("audio")
	if err != nil || data[0] != 'b' {
		t.Fatal("old publication lost", err)
	}
}

func TestS3ConditionalCreateCancellationAndPagination(t *testing.T) {
	base := t.TempDir()
	backend := teststorage.New(t, base, nil)
	root, err := backend.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	first, err := root.OpenFile("exclusive", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	second, err := root.OpenFile("exclusive", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	first.Write([]byte("winner"))
	second.Write([]byte("loser"))
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	if err = second.Close(); !errors.Is(err, os.ErrExist) {
		t.Fatalf("exclusive publication: %v", err)
	}
	data, err := root.ReadFile("exclusive")
	if err != nil || string(data) != "winner" {
		t.Fatal(string(data), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	staged, err := root.WithContext(ctx).OpenFile("cancelled", os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		t.Fatal(err)
	}
	staged.Write([]byte("private"))
	cancel()
	if err = staged.Close(); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled close: %v", err)
	}
	if _, err = root.Stat("cancelled"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	// More than one service page, including markers and object names requiring URL encoding.
	for i := 0; i < 1005; i++ {
		if err = root.WriteAtomic(fmt.Sprintf("page-%04d", i), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	var count int
	for {
		entries, err := dir.ReadDir(37)
		count += len(entries)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if count != 1006 {
		t.Fatalf("pagination returned %d entries", count)
	}
	if _, err = backend.Open(filepath.Join(base, "..", "outside")); !errors.Is(err, os.ErrPermission) {
		t.Fatal("outside-base access", err)
	}
}

func TestS3DirectoryCopyFailureRetainsSources(t *testing.T) {
	base := t.TempDir()
	var copies atomic.Int32
	backend := teststorage.New(t, base, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Amz-Copy-Source") != "" && copies.Add(1) == 2 {
				http.Error(w, "injected copy failure", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	root, err := backend.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = root.MkdirAll("source", 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		if err = root.WriteAtomic("source/"+name, []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = root.Rename("source", "destination"); err == nil {
		t.Fatal("copy failure accepted")
	}
	for _, name := range []string{"a", "b"} {
		if data, err := root.ReadFile("source/" + name); err != nil || string(data) != name {
			t.Fatal("lost source", name, err)
		}
	}
}
