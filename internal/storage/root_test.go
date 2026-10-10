// SPDX-License-Identifier: GPL-2.0-only
package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfinedAtomicWrite(t *testing.T) {
	base, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "protected"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "escape")); err != nil {
		t.Skip(err)
	}
	r, err := Open(base)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, name := range []string{"../outside", "escape/protected"} {
		if err := r.WriteAtomic(name, []byte("bad"), 0600); err == nil {
			t.Fatal("escape accepted", name)
		}
	}
	data, _ := os.ReadFile(filepath.Join(outside, "protected"))
	if string(data) != "original" {
		t.Fatal("outside changed")
	}
	if err := r.WriteAtomic("state", []byte("valid"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err = r.ReadFile("state")
	if err != nil || string(data) != "valid" {
		t.Fatal(string(data), err)
	}
}

func TestLockCancellationReleasesPartialAcquisition(t *testing.T) {
	root, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	held := root.Lock("b")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if unlock, err := root.LockContext(ctx, "a", "b"); err == nil {
		unlock()
		t.Fatal("cancelled lock accepted")
	}
	held()
	unlock, err := root.LockContext(context.Background(), "a", "b")
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	resources.Lock()
	defer resources.Unlock()
	if len(resources.locks) != 0 {
		t.Fatal("lock entries leaked")
	}
}

func TestStreamAtomicOverflowAndCancellationPreserveDestination(t *testing.T) {
	root, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := root.WriteAtomic("state", []byte("valid"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteReaderAtomic(context.Background(), "state", strings.NewReader("too large"), 0600, 3); err == nil {
		t.Fatal("overflow accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := root.WriteReaderAtomic(ctx, "state", strings.NewReader("new"), 0600, 3); err == nil {
		t.Fatal("cancellation accepted")
	}
	data, err := root.ReadFile("state")
	if err != nil || string(data) != "valid" {
		t.Fatal("valid file replaced", err)
	}
	dir, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil || len(entries) != 1 {
		t.Fatal("private files leaked", entries, err)
	}
	if err := root.WriteReaderAtomic(context.Background(), "state", strings.NewReader("new"), 0600, 3); err != nil {
		t.Fatal(err)
	}
	data, err = root.ReadFile("state")
	if err != nil || string(data) != "new" {
		t.Fatal(data, err)
	}
}

func TestRootHandlesShareResourceCoordination(t *testing.T) {
	path := t.TempDir()
	if err := os.Mkdir(filepath.Join(path, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	outer, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer outer.Close()
	inner, err := Open(filepath.Join(path, "nested"))
	if err != nil {
		t.Fatal(err)
	}
	defer inner.Close()
	other, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	held := outer.Lock("nested/file")
	defer held()
	for _, target := range []struct {
		root *Root
		name string
	}{{inner, "file"}, {other, "nested/file"}} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		release, err := target.root.LockContext(ctx, target.name)
		cancel()
		if release != nil {
			release()
		}
		if err == nil {
			t.Fatal("separate root handle bypassed lock")
		}
	}
}

func TestAliasedRootsShareResourceCoordination(t *testing.T) {
	path := t.TempDir()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(path, alias); err != nil {
		t.Skip(err)
	}
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(alias)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	release := a.Lock("file")
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if unlock, err := b.LockContext(ctx, "file"); err == nil {
		unlock()
		t.Fatal("alias bypassed lock")
	}
}
