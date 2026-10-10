// SPDX-License-Identifier: GPL-2.0-only
package storage_test

import (
	"os"
	"path/filepath"
	"testing"

	"goteddycloud/internal/storage"
	"goteddycloud/internal/teststorage"
)

func TestScopedBackendIsolation(t *testing.T) {
	for _, kind := range []string{"filesystem", "s3"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			var backend storage.Backend = storage.Filesystem{}
			if kind == "s3" {
				backend = teststorage.New(t, base, nil)
			}
			alice, err := storage.NewScoped(backend, filepath.Join(base, "alice"))
			if err != nil {
				t.Fatal(err)
			}
			defer alice.Close()
			bob, err := storage.NewScoped(backend, filepath.Join(base, "bob"))
			if err != nil {
				t.Fatal(err)
			}
			defer bob.Close()
			bobRoot, err := bob.OpenRoot(filepath.Join(base, "bob"))
			if err != nil {
				t.Fatal(err)
			}
			defer bobRoot.Close()
			if err := bobRoot.WriteAtomic("secret", []byte("bob"), 0600); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{base, filepath.Join(base, "bob"), filepath.Join(base, "alice-sibling"), "../bob"} {
				if root, err := alice.OpenRoot(path); err == nil {
					root.Close()
					t.Fatalf("opened %s", path)
				}
				if err := alice.MkdirAll(path, 0700); err == nil {
					t.Fatalf("created %s", path)
				}
			}
			secret := filepath.Join(base, "bob", "secret")
			if _, err := alice.ReadFile(secret); err == nil {
				t.Fatal("read another area")
			}
			alice.AllowReadFiles([]string{secret})
			if data, err := alice.ReadFile(secret); err != nil || string(data) != "bob" {
				t.Fatalf("shared read: %s %v", data, err)
			}
			if root, err := alice.OpenRoot(filepath.Dir(secret)); err == nil {
				root.Close()
				t.Fatal("shared parent granted")
			}
			alice.AllowReadFiles(nil)
			if _, err := alice.ReadFile(secret); err == nil {
				t.Fatal("shared read not revoked")
			}
			if kind == "filesystem" {
				if err := os.Symlink(filepath.Join(base, "bob"), filepath.Join(base, "alice", "escape")); err != nil {
					t.Fatal(err)
				}
				if root, err := alice.OpenRoot(filepath.Join(base, "alice", "escape")); err == nil {
					root.Close()
					t.Fatal("symlink escape")
				}
				if _, err := alice.ReadFile(filepath.Join(base, "alice", "escape", "secret")); err == nil {
					t.Fatal("symlink file escape")
				}
				if err := alice.MkdirAll(filepath.Join(base, "alice", "escape", "directory"), 0700); err == nil {
					t.Fatal("symlink mkdir escape")
				}
			}
		})
	}
}
