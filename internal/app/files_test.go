// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestFileMutationsConflictsAndConfinement(t *testing.T) {
	s := fixtureApp(t)
	snap := s.Settings.Snapshot("")
	library := snap.String("internal.librarydirfull")
	for _, tc := range []struct {
		endpoint, body string
		status         int
	}{
		{"dirCreate", "new", 200}, {"dirCreate", "new", 500},
		{"dirCreate", "new/child", 200},
		{"fileMove", "source=new/child&target=new/moved", 200},
		{"fileMove", "source=new/moved&target=new/child", 200}, {"dirDelete", "new", 500},
		{"dirDelete", "new/child", 200}, {"dirDelete", "new", 200},
		{"fileDelete", "missing", 500}, {"fileMove", "source=missing&target=other", 500},
		{"dirCreate", "../escape", 400}, {"dirDelete", "/", 400}, {"fileDelete", ".", 400},
		{"fileMove", "source=../escape&target=other", 400},
	} {
		result := perform(s, "POST", "/api/"+tc.endpoint+"?special=library", []byte(tc.body), false)
		if result.Code != tc.status {
			t.Fatalf("%s %q: %d %q", tc.endpoint, tc.body, result.Code, result.Body.String())
		}
	}
	for _, name := range []string{"one", "two"} {
		if err := os.WriteFile(filepath.Join(library, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	result := perform(s, "POST", "/api/fileMove?special=library", []byte("source=one&target=two"), false)
	if result.Code != 500 {
		t.Fatal("collision allowed", result.Code)
	}
	data, _ := os.ReadFile(filepath.Join(library, "two"))
	if string(data) != "two" {
		t.Fatal("target overwritten")
	}
	var wg sync.WaitGroup
	results := make(chan int, 2)
	for _, name := range []string{"one", "two"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			results <- perform(s, "POST", "/api/fileMove?special=library", []byte("source="+name+"&target=winner"), false).Code
		}(name)
	}
	wg.Wait()
	close(results)
	success, failure := 0, 0
	for result := range results {
		if result == 200 {
			success++
		} else if result == 500 {
			failure++
		}
	}
	if success != 1 || failure != 1 {
		t.Fatal("move conflict", success, failure)
	}
	result = perform(s, "POST", "/api/fileDelete?special=library", []byte("winner"), false)
	if result.Code != 200 {
		t.Fatal(result.Code)
	}
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "file")
	if err := os.WriteFile(sentinel, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(library, "escape")); err != nil {
		t.Skip(err)
	}
	for _, tc := range []struct{ endpoint, body string }{{"fileDelete", "escape/file"}, {"dirCreate", "escape/new"}, {"fileMove", "source=escape/file&target=outside"}} {
		if result := perform(s, "POST", "/api/"+tc.endpoint+"?special=library", []byte(tc.body), false); result.Code == 200 {
			t.Fatal("escape accepted", tc)
		}
	}
	data, _ = os.ReadFile(sentinel)
	if string(data) != "private" {
		t.Fatal("outside changed")
	}
}

func TestMigrationPublicationRootModeAndCopyCancellation(t *testing.T) {
	s := fixtureApp(t)
	snap := s.Settings.Snapshot("")
	const ruid = "0200000002000000"
	data := syntheticTAF(t)
	path := filepath.Join(snap.String("internal.contentdirfull"), "02000000", "02000000")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	result := perform(s, "POST", "/api/migrateContent2Lib", []byte("ruid="+ruid+"&libroot=true"), false)
	if result.Code != 200 {
		t.Fatal(result.Code, result.Body.String())
	}
	metadata, err := s.Content.Metadata(snap, ruid)
	if err != nil || metadata.String("source") != "lib://123456.taf" {
		t.Fatal(metadata, err)
	}
	migrated, err := os.ReadFile(filepath.Join(snap.String("internal.librarydirfull"), "123456.taf"))
	if err != nil || !bytes.Equal(data, migrated) {
		t.Fatal("migration changed bytes", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("old UID retained", err)
	}
	if result := perform(s, "POST", "/api/migrateContent2Lib", []byte("ruid="+ruid), false); result.Code != 404 {
		t.Fatal("mapped source migrated", result.Code)
	}
	root, _, err := s.Content.ManagedRoot(snap, "library")
	if err != nil {
		t.Fatal(err)
	}
	if err := root.WriteAtomic("source", data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("POST", "/", nil).WithContext(ctx)
	if err := copyThenRemove(req, root, "source", "target"); err == nil {
		t.Fatal("canceled move accepted")
	}
	if _, err := root.Stat("target"); !os.IsNotExist(err) {
		t.Fatal("partial target published", err)
	}
	remaining, err := root.ReadFile("source")
	if err != nil || !bytes.Equal(remaining, data) {
		t.Fatal("source lost", err)
	}
	req = httptest.NewRequest("POST", "/", nil)
	if err := copyThenRemove(req, root, "source", "target"); err != nil {
		t.Fatal(err)
	}
	remaining, err = root.ReadFile("target")
	if err != nil || !bytes.Equal(remaining, data) {
		t.Fatal("copy changed bytes", err)
	}
	if _, err := root.Stat("source"); !os.IsNotExist(err) {
		t.Fatal("source retained", err)
	}
}
