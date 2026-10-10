// SPDX-License-Identifier: GPL-2.0-only
package cc3200cli

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"goteddycloud/internal/cc3200"
)

func imageFixture(t *testing.T) string {
	t.Helper()
	image := bytes.Repeat([]byte{255}, 8*cc3200.BlockSize)
	binary.LittleEndian.PutUint16(image, 1)
	binary.LittleEndian.PutUint16(image[2:], 0x534c)
	copy(image[4:], []byte{0, 1, 5, 0xc0})
	name := "/cert/ca.der"
	binary.LittleEndian.PutUint16(image[0x774:], 0)
	binary.LittleEndian.PutUint16(image[0x776:], uint16(len(name)))
	copy(image[0x974:], name)
	start := 5 * cc3200.BlockSize
	copy(image[start:], []byte{4, 0, 0, 0, 0, 0, 0, 0})
	copy(image[start+8:], []byte{1, 2, 3, 4})
	path := filepath.Join(t.TempDir(), "flash.bin")
	if err := os.WriteFile(path, image, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestOfflineCLIReadsExtractsAndPatches(t *testing.T) {
	image := imageFixture(t)
	ctx := context.Background()
	var stdout, stderr bytes.Buffer
	if err := Run(ctx, []string{"-if", image, "list_filesystem"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("/cert/ca.der")) {
		t.Fatal(stdout.String())
	}
	stdout.Reset()
	if err := Run(ctx, []string{"-if", image, "read_file", "/cert/ca.der", "-"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stdout.Bytes(), []byte{1, 2, 3, 4}) {
		t.Fatal(stdout.Bytes())
	}
	directory := filepath.Join(t.TempDir(), "extracted")
	if err := Run(ctx, []string{"-if", image, "read_all_files", directory}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(filepath.Join(directory, "cert/ca.der"))
	if err != nil || !bytes.Equal(actual, []byte{1, 2, 3, 4}) {
		t.Fatal(actual, err)
	}
	replacement := filepath.Join(t.TempDir(), "replacement.der")
	if err = os.WriteFile(replacement, []byte{8, 9}, 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "new.bin")
	args := []string{"-if", image, "-of", output, "--yes", "write_file", replacement, "/cert/ca.der"}
	if err = Run(ctx, args, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	data, err := cc3200.ImageFile(updated, cc3200.ImageOptions{}, "/cert/ca.der", -1)
	if err != nil || !bytes.Equal(data, []byte{8, 9}) {
		t.Fatal(data, err)
	}
	if err = Run(ctx, args, &stdout, &stderr); err == nil {
		t.Fatal("overwrote existing image")
	}
}
func TestCLIMutationPreflightAndBounds(t *testing.T) {
	for _, args := range [][]string{
		{"--port", "does-not-exist", "write_file", "a", "/cert/ca.der"},
		{"--port", "does-not-exist", "--yes", "format_flash"},
		{"--image_file", imageFixture(t), "read_flash", "--offset", "17M", "-"},
		{"--image_file", imageFixture(t), "read_flash", "--offset", "1M", "-"},
	} {
		var stdout, stderr bytes.Buffer
		if err := Run(context.Background(), args, &stdout, &stderr); err == nil {
			t.Fatal(args)
		}
	}
}
func TestBulkUploadRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(imageFixture(t), filepath.Join(root, "ca.der")); err != nil {
		t.Skip(err)
	}
	if _, err := readDirectory(root); err == nil {
		t.Fatal("accepted symlink")
	}
}
