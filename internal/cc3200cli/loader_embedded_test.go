//go:build cc3200_embedded_loader

// SPDX-License-Identifier: GPL-2.0-only
package cc3200cli

import (
	"bytes"
	"os"
	"testing"

	"goteddycloud/internal/cc3200"
)

func TestEmbeddedLoaderIsIndependentOfLocalFiles(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("rbtl3100s.dll", []byte("stray local file"), 0600); err != nil {
		t.Fatal(err)
	}
	data, source, err := loadLoader("")
	if err != nil || source != "embedded rbtl3100s.dll" || len(data) != 49588 || cc3200.Digest(data) != cc3200.LoaderSHA256 {
		t.Fatal("embedded stock loader unavailable", source, len(data), err)
	}
	data[0] ^= 255
	again, _, err := loadLoader("")
	if err != nil || !bytes.Equal(again, embeddedLoader) {
		t.Fatal("caller mutated embedded loader", err)
	}
}
