// SPDX-License-Identifier: GPL-2.0-only
package storage

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func FuzzConfinedRead(f *testing.F) {
	base := f.TempDir()
	inside := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(inside, 0700); err != nil {
		f.Fatal(err)
	}
	if err := os.Mkdir(outside, 0700); err != nil {
		f.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inside, "allowed"), []byte("inside"), 0600); err != nil {
		f.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("outside sentinel"), 0600); err != nil {
		f.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(inside, "escape")); err != nil {
		f.Skip("symlink support required")
	}
	root, err := Open(inside)
	if err != nil {
		f.Fatal(err)
	}
	defer root.Close()
	for _, seed := range []string{"allowed", "../outside/secret", "escape/secret", "/etc/passwd", "..\\outside\\secret", "\x00", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		if len(name) > 2048 {
			return
		}
		file, err := root.Open(name)
		if err != nil {
			return
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, 32))
		if err == nil && string(data) != "inside" {
			t.Fatalf("confined root disclosed unexpected file")
		}
	})
}
