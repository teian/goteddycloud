// SPDX-License-Identifier: GPL-2.0-only
package cc3200cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplicitLoaderOverrideAndDiagnostics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future-loader.bin")
	data := []byte("explicit future loader")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	actual, source, err := loadLoader(path)
	if err != nil || source != path || !bytes.Equal(actual, data) {
		t.Fatal("override not selected", source, err)
	}
	var stdout, stderr bytes.Buffer
	if err := Run(context.Background(), []string{"--loader", path, "dll_data_test"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), path) || !strings.Contains(stderr.String(), "SHA256") {
		t.Fatal("diagnostic missing loader source or hash", stderr.String())
	}
}

func TestExplicitInvalidLoaderNeverFallsBack(t *testing.T) {
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"missing", nil}, {"empty", []byte{}}, {"oversized", make([]byte, (128<<10)+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "loader.bin")
			if test.data != nil {
				if err := os.WriteFile(path, test.data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, source, err := loadLoader(path); err == nil || source != path {
				t.Fatal("invalid explicit loader fell back", source, err)
			}
		})
	}
}
