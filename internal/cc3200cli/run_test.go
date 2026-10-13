// SPDX-License-Identifier: GPL-2.0-only
package cc3200cli

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
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
	if err := Run(ctx, []string{"-if", image, "list_filesystem", "--json-output"}, &stdout, &stderr); err != nil {
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

func TestOfflineChainedCommandsAndAnonymousExtraction(t *testing.T) {
	image := imageFixture(t)
	data, e := os.ReadFile(image)
	if e != nil {
		t.Fatal(e)
	}
	binary.LittleEndian.PutUint16(data[0x774:], 0xffff)
	binary.LittleEndian.PutUint16(data[0x776:], 0xffff)
	if e = os.WriteFile(image, data, 0600); e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(t.TempDir(), "extracted")
	var stdout, stderr bytes.Buffer
	if e = Run(context.Background(), []string{"-if", image, "read_all_files", dir, "--by-file-id", "read_file", "unused-name", "-", "--file-id", "0"}, &stdout, &stderr); e != nil {
		t.Fatal(e, stderr.String())
	}
	extracted, e := os.ReadFile(filepath.Join(dir, "0"))
	if e != nil || !bytes.Equal(extracted, []byte{1, 2, 3, 4}) || !bytes.Equal(extracted, stdout.Bytes()) {
		t.Fatal(extracted, stdout.Bytes(), e)
	}

	image = imageFixture(t)
	input := filepath.Join(t.TempDir(), "new.der")
	if e = os.WriteFile(input, []byte{8, 9}, 0600); e != nil {
		t.Fatal(e)
	}
	output := filepath.Join(t.TempDir(), "patched.bin")
	stdout.Reset()
	stderr.Reset()
	if e = Run(context.Background(), []string{"-if", image, "-of", output, "write_file", input, "/cert/ca.der", "read_file", "/cert/ca.der", "-"}, &stdout, &stderr); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(stdout.Bytes(), []byte{8, 9}) {
		t.Fatal(stdout.Bytes())
	}
	patched, e := os.ReadFile(output)
	if e != nil {
		t.Fatal(e)
	}
	actual, e := cc3200.ImageFile(patched, cc3200.ImageOptions{}, "/cert/ca.der", -1)
	if e != nil || !bytes.Equal(actual, stdout.Bytes()) {
		t.Fatal(actual, e)
	}
}

func TestChainPreflightPreventsEarlierSideEffects(t *testing.T) {
	image := imageFixture(t)
	output := filepath.Join(t.TempDir(), "read.der")
	var stdout, stderr bytes.Buffer
	e := Run(context.Background(), []string{"-if", image, "read_file", "/cert/ca.der", output, "write_all_files", "missing-input-directory"}, &stdout, &stderr)
	if e == nil {
		t.Fatal("accepted invalid later command")
	}
	if _, e = os.Stat(output); !os.IsNotExist(e) {
		t.Fatal("executed earlier command before completing preflight", e)
	}
}

func TestFilesystemHumanAndPythonJSONOutput(t *testing.T) {
	image := imageFixture(t)
	var stdout, stderr bytes.Buffer
	if e := Run(context.Background(), []string{"-if", image, "list_filesystem", "--extended"}, &stdout, &stderr); e != nil {
		t.Fatal(e)
	}
	if stdout.Len() != 0 || !bytes.Contains(stderr.Bytes(), []byte("/cert/ca.der")) || !bytes.Contains(stderr.Bytes(), []byte("BYTES")) {
		t.Fatal(stdout.String(), stderr.String())
	}
	stderr.Reset()
	if e := Run(context.Background(), []string{"--quiet", "-if", image, "list_filesystem", "--json-output"}, &stdout, &stderr); e != nil {
		t.Fatal(e)
	}
	const want = `{"fat_commit_revision":1,"block_size":4096,"block_count":8,"used_blocks":6,"files":[{"index":0,"start_block":5,"size_blocks":1,"mirrored":false,"flags":12,"fname":"/cert/ca.der","total_blocks":1,"header":null,"magic":null,"size":0}],"holes":[{"start_block":6,"size_blocks":1}]}`
	if strings.TrimSpace(stdout.String()) != want || stderr.Len() != 0 {
		t.Fatal(stdout.String(), stderr.String())
	}
}
