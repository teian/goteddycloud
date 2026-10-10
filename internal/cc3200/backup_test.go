// SPDX-License-Identifier: GPL-2.0-only
package cc3200

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func testFlash() []byte {
	data := bytes.Repeat([]byte{255}, 8*BlockSize)
	binary.LittleEndian.PutUint16(data, 1)
	binary.LittleEndian.PutUint16(data[2:], 0x534c)
	copy(data[4:], []byte{0, 1, 5, 0x80}) // Anonymous mirrored allocation.
	return data
}
func testZIP(t *testing.T, names []string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, name := range names {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte("content"))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func testARM(marker string) []byte {
	data := make([]byte, 256)
	binary.LittleEndian.PutUint32(data, 0x200040d0)
	binary.LittleEndian.PutUint32(data[4:], 0x20004065)
	copy(data[64:], marker)
	return data
}
func TestBackupPreservesAnonymousMirrorsAndNamedFiles(t *testing.T) {
	flash := testFlash()
	archive, m, err := BuildBackup(flash, testZIP(t, []string{"cert/client.der"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Entries) != 1 || m.Entries[0].Name != "" || !m.Entries[0].Mirrored || m.ID != Digest(archive) || len(m.Missing) != 4 {
		t.Fatal(m)
	}
	z, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	objects := map[string][]byte{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		objects[f.Name], err = io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(objects["flash.bin"], flash) || !bytes.Equal(objects["allocations/000-0.bin"], flash[5*BlockSize:6*BlockSize]) || !bytes.Equal(objects["allocations/000-1.bin"], flash[6*BlockSize:7*BlockSize]) {
		t.Fatal("forensic allocations not preserved")
	}
	manifest, err := BackupManifest(archive)
	if err != nil || len(manifest.Missing) != 4 || manifest.FlashSHA256 != Digest(flash) {
		t.Fatal(manifest, err)
	}
	files, err := BackupFiles(archive)
	if err != nil || string(files["/cert/client.der"]) != "content" {
		t.Fatal(files, err)
	}
}
func TestInspectSelectsMatchingMetadataAndRejectsCorruption(t *testing.T) {
	data := testFlash()
	copy(data[BlockSize:2*BlockSize], data[:BlockSize])
	binary.LittleEndian.PutUint16(data[BlockSize:], 2)
	at := BlockSize + 0x774
	binary.LittleEndian.PutUint16(data[at:], 0)
	binary.LittleEndian.PutUint16(data[at+2:], uint16(len("/named")))
	copy(data[BlockSize+0x974:], "/named")
	m, err := Inspect(data)
	if err != nil || m.Revision != 2 || m.Entries[0].Name != "/named" {
		t.Fatal(m, err)
	}
	// Filename storage extends beyond the first 4 KiB of each FAT metadata area.
	binary.LittleEndian.PutUint16(data[at:], 2000)
	copy(data[BlockSize+0x974+2000:], "/named")
	if _, err := Inspect(data); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func([]byte){func(b []byte) { b[BlockSize+4] = 1 }, func(b []byte) { b[BlockSize+6] = 4 }, func(b []byte) { b[BlockSize+5] = 255 }, func(b []byte) { copy(b[BlockSize+8:], []byte{1, 1, 5, 0xc0}) }, func(b []byte) { binary.LittleEndian.PutUint16(b[at:], 65534) }} {
		bad := append([]byte(nil), data...)
		mutate(bad)
		if _, err := Inspect(bad); err == nil {
			t.Fatal("corrupt FAT accepted")
		}
	}
}
func TestNamedArchiveConfinement(t *testing.T) {
	for _, names := range [][]string{{"../private"}, {"/cert/client.der"}, {"cert/../private"}, {"a\\b"}, {"same", "same"}, {"directory/"}} {
		if _, err := ReadNamedZIP(testZIP(t, names)); err == nil {
			t.Fatal("unsafe archive accepted", names)
		}
	}
}
func TestInstallationPlanValidatesOriginalAndPreloader(t *testing.T) {
	original, preloader := testARM("original"), testARM("HackieboxNG preloader nodebug")
	files := map[string][]byte{}
	for _, name := range ImportantFiles() {
		files[name] = []byte("material")
	}
	files["/sys/mcuimg.bin"] = original
	writes, err := Plan(files, preloader, []byte("DER"), Digest(original))
	if err != nil || len(writes) != 3 || writes[0].Path != "/sys/pre-img.bin" || writes[2].Path != "/sys/mcuimg.bin" {
		t.Fatal(writes, err)
	}
	if _, err := Plan(files, preloader, nil, Digest(original)); err == nil {
		t.Fatal("missing CA accepted")
	}
	if _, err := Plan(files, preloader, []byte("DER"), Digest(preloader)); err == nil {
		t.Fatal("wrong original hash accepted")
	}
	if _, err := Plan(files, original, []byte("DER"), Digest(original)); err == nil {
		t.Fatal("original accepted as preloader")
	}
	files["/sys/pre-img.bin"] = original
	files["/sys/mcuimg.bin"] = preloader
	if _, err := Plan(files, preloader, []byte("DER"), Digest(original)); err != nil {
		t.Fatal("update lost preserved original", err)
	}
	delete(files, "/cert/private.der")
	if _, err := Plan(files, preloader, []byte("DER"), Digest(original)); err == nil {
		t.Fatal("incomplete backup accepted")
	}
}
func FuzzInspect(f *testing.F) {
	f.Add(testFlash())
	f.Add([]byte{0, 1, 2})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) <= MaxFlash {
			Inspect(data)
		}
	})
}
