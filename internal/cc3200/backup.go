// SPDX-License-Identifier: GPL-2.0-only
// Package cc3200 inspects CC3200 serial flash backups without modifying them.
package cc3200

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

const MaxFlash = 16 << 20
const MaxArchive = 17 << 20
const MaxBackup = 3*MaxFlash + (1 << 20)
const BlockSize = 4096

func ImportantFiles() []string {
	return []string{"/cert/ca.der", "/cert/client.der", "/cert/private.der", "/sys/mcuimg.bin", "/sys/mcubootinfo.bin"}
}

type Entry struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Start    int    `json:"start"`
	Blocks   int    `json:"blocks"`
	Mirrored bool   `json:"mirrored"`
}
type Manifest struct {
	ID          string   `json:"id,omitempty"`
	FlashSHA256 string   `json:"flashSHA256"`
	Revision    uint16   `json:"revision"`
	Entries     []Entry  `json:"entries"`
	Files       []string `json:"files"`
	Missing     []string `json:"missing"`
}

func Digest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// Inspect uses the newest valid FAT copy, including its own filename metadata.
// Unknown names stay unknown; their allocations are retained by ID in backups.
func Inspect(flash []byte) (Manifest, error) {
	return InspectImage(flash, ImageOptions{})
}

// ImageOptions selects the inactive FAT or the partial CC32xx image layout.
type ImageOptions struct{ Inactive, CC32XX bool }

func InspectImage(flash []byte, options ImageOptions) (Manifest, error) {
	m := Manifest{FlashSHA256: Digest(flash), Entries: []Entry{}, Files: []string{}, Missing: []string{}}
	if len(flash) < 5*BlockSize || len(flash) > MaxFlash || len(flash)%BlockSize != 0 {
		return m, errors.New("invalid CC3200 flash geometry")
	}
	selected := -1
	for i := 0; i < 2; i++ {
		fat := flash[i*BlockSize : (i+1)*BlockSize]
		rev := binary.LittleEndian.Uint16(fat)
		if binary.LittleEndian.Uint16(fat[2:]) == 0x534c && rev != 0xffff && (selected < 0 || rev > m.Revision) {
			selected = i
			m.Revision = rev
		}
	}
	if selected < 0 {
		return m, errors.New("no valid CC3200 FAT")
	}
	if options.Inactive {
		other := 1 - selected
		fat := flash[other*BlockSize : (other+1)*BlockSize]
		revision := binary.LittleEndian.Uint16(fat)
		if binary.LittleEndian.Uint16(fat[2:]) != 0x534c || revision == 0xffff {
			return m, errors.New("no valid inactive FAT")
		}
		selected = other
		m.Revision = revision
	}
	fat := flash[selected*BlockSize : (selected+1)*BlockSize]
	metadataOffset, nameOffset := 0x774, 0x200
	if options.CC32XX {
		metadataOffset, nameOffset = 0x2000, 0x3c0
	}
	metadata := flash[selected*BlockSize+metadataOffset : selected*BlockSize+metadataOffset+0x1000]
	occupied := make([]bool, len(flash)/BlockSize)
	for i := 0; i < 5; i++ {
		occupied[i] = true
	}
	names := map[string]bool{}
	for id := 0; id < 128; id++ {
		meta := fat[4+id*4 : 8+id*4]
		if bytes.Equal(meta, []byte{255, 255, 255, 255}) || bytes.Equal(meta, []byte{255, byte(id), 255, 127}) {
			continue
		}
		if int(meta[0]) != id || meta[1] == 0 {
			return m, errors.New("invalid FAT allocation")
		}
		e := Entry{ID: id, Start: int(meta[2]) + int(meta[3]&15)*256, Blocks: int(meta[1]), Mirrored: meta[3]&0x40 == 0}
		blocks := e.Blocks
		if e.Mirrored {
			blocks *= 2
		}
		if e.Start < 5 || e.Start+blocks > len(occupied) {
			return m, errors.New("FAT allocation outside flash")
		}
		for b := e.Start; b < e.Start+blocks; b++ {
			if occupied[b] {
				return m, errors.New("overlapping FAT allocations")
			}
			occupied[b] = true
		}
		at := id * 4
		off := int(binary.LittleEndian.Uint16(metadata[at:]))
		size := int(binary.LittleEndian.Uint16(metadata[at+2:]))
		if off != 0xffff || size != 0xffff {
			start := nameOffset + off
			if size == 0 || size > 255 || start+size > len(metadata) {
				return m, errors.New("invalid FAT filename bounds")
			}
			e.Name = strings.TrimRight(string(metadata[start:start+size]), "\x00")
			if !ValidName(e.Name) || names[e.Name] {
				return m, errors.New("invalid or duplicate FAT filename")
			}
			names[e.Name] = true
		}
		m.Entries = append(m.Entries, e)
	}
	return m, nil
}
func ValidName(name string) bool {
	return len(name) > 1 && len(name) <= 255 && utf8.ValidString(name) && name[0] == '/' && path.Clean(name) == name && !strings.ContainsAny(name, "\\:\x00\r\n") && !strings.Contains(name, "//")
}

// BuildBackup retains the complete flash, every anonymous/mirrored allocation,
// and the named files independently read through the device filesystem API.
func BuildBackup(flash, namedZIP []byte) ([]byte, Manifest, error) {
	m, err := Inspect(flash)
	if err != nil {
		return nil, m, err
	}
	files, err := ReadNamedZIP(namedZIP)
	if err != nil {
		return nil, m, err
	}
	required := ImportantFiles()
	for _, entry := range m.Entries {
		if entry.Name != "" {
			required = append(required, entry.Name)
		}
	}
	missing := map[string]bool{}
	for _, name := range required {
		if _, ok := files[name]; !ok && !missing[name] {
			missing[name] = true
			m.Missing = append(m.Missing, name)
		}
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	m.Files = names
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	add := func(name string, data []byte) error {
		h := &zip.FileHeader{Name: name, Method: zip.Store}
		h.SetMode(0600)
		w, err := z.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	if err = add("flash.bin", flash); err != nil {
		return nil, m, err
	}
	for _, name := range names {
		if err = add("files"+name, files[name]); err != nil {
			return nil, m, err
		}
	}
	for _, e := range m.Entries {
		for copy := 0; copy < 1+boolInt(e.Mirrored); copy++ {
			start := (e.Start + copy*e.Blocks) * BlockSize
			allocation := flash[start : start+e.Blocks*BlockSize]
			if err = add(fmt.Sprintf("allocations/%03d-%d.bin", e.ID, copy), allocation); err != nil {
				return nil, m, err
			}
		}
	}
	manifest, _ := json.Marshal(m)
	if err = add("manifest.json", manifest); err != nil {
		return nil, m, err
	}
	if err = z.Close(); err != nil {
		return nil, m, err
	}
	if out.Len() > MaxBackup {
		return nil, m, errors.New("backup exceeds size limit")
	}
	m.ID = Digest(out.Bytes())
	return out.Bytes(), m, nil
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// ReadNamedZIP rejects traversal, aliases, duplicates and decompression bombs.
func ReadNamedZIP(data []byte) (map[string][]byte, error) {
	result := map[string][]byte{}
	if len(data) == 0 {
		return result, nil
	}
	if len(data) > MaxArchive {
		return nil, errors.New("file archive too large")
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("invalid file archive")
	}
	if len(z.File) > 256 {
		return nil, errors.New("too many archived files")
	}
	total := 0
	for _, f := range z.File {
		name := "/" + f.Name
		if !ValidName(name) || f.FileInfo().IsDir() || !f.Mode().IsRegular() || strings.HasPrefix(f.Name, "/") || f.UncompressedSize64 > MaxFlash {
			return nil, errors.New("invalid archived file")
		}
		if _, exists := result[name]; exists {
			return nil, errors.New("duplicate archived file")
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(r, int64(MaxFlash-total)+1))
		r.Close()
		total += len(b)
		if err != nil || total > MaxFlash {
			return nil, errors.New("file archive exceeds limit or has invalid CRC")
		}
		result[name] = b
	}
	return result, nil
}
func BackupFiles(data []byte) (map[string][]byte, error) {
	if len(data) > MaxBackup {
		return nil, errors.New("backup too large")
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	if len(z.File) > 514 {
		return nil, errors.New("too many backup records")
	}
	files := map[string][]byte{}
	total := 0
	for _, f := range z.File {
		if !strings.HasPrefix(f.Name, "files/") {
			continue
		}
		name := strings.TrimPrefix(f.Name, "files")
		if !ValidName(name) || !f.Mode().IsRegular() || f.UncompressedSize64 > MaxFlash {
			return nil, errors.New("invalid backup file")
		}
		if _, exists := files[name]; exists {
			return nil, errors.New("duplicate backup file")
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(r, int64(MaxFlash-total)+1))
		r.Close()
		total += len(data)
		if err != nil || total > MaxFlash {
			return nil, errors.New("backup file limit or CRC failure")
		}
		files[name] = data
	}
	return files, nil
}

func BackupManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	if len(data) > MaxBackup {
		return manifest, errors.New("backup too large")
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return manifest, err
	}
	for _, file := range z.File {
		if file.Name != "manifest.json" {
			continue
		}
		if file.UncompressedSize64 > 64<<10 {
			return manifest, errors.New("backup manifest too large")
		}
		r, err := file.Open()
		if err != nil {
			return manifest, err
		}
		data, err := io.ReadAll(io.LimitReader(r, (64<<10)+1))
		r.Close()
		if err != nil || len(data) > 64<<10 {
			return manifest, errors.New("invalid backup manifest")
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			return manifest, err
		}
		return manifest, nil
	}
	return manifest, errors.New("backup manifest missing")
}

type Write struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Data   []byte `json:"data"`
}

func ARMImage(data []byte) bool {
	if len(data) < 64 || len(data) > 0x3c000 {
		return false
	}
	sp := binary.LittleEndian.Uint32(data)
	pc := binary.LittleEndian.Uint32(data[4:])
	return sp >= 0x20000000 && sp <= 0x20040000 && sp%4 == 0 && pc&1 == 1 && pc&^1 >= 0x20004000 && uint64(pc&^1) < 0x20004000+uint64(len(data))
}
func Plan(files map[string][]byte, preloader, ca []byte, originalHash string) ([]Write, error) {
	original := files["/sys/pre-img.bin"]
	if len(original) == 0 {
		original = files["/sys/mcuimg.bin"]
	}
	if !ARMImage(original) || strings.Contains(string(original), "HackieboxNG") || len(originalHash) != 64 || Digest(original) != strings.ToLower(originalHash) {
		return nil, errors.New("original bootloader must match the SHA-256 of a trusted original backup and be a CC3200 ARM image")
	}
	if !ARMImage(preloader) || !bytes.Contains(preloader, []byte("HackieboxNG preloader")) || bytes.Equal(original, preloader) {
		return nil, errors.New("select flash/sys/mcuimg.bin from a HackieboxNG release")
	}
	for _, name := range ImportantFiles() {
		if len(files[name]) == 0 {
			return nil, fmt.Errorf("required backup file missing: %s", name)
		}
	}
	if len(ca) == 0 {
		return nil, errors.New("replacement CA unavailable")
	}
	writes := []Write{}
	for _, item := range []struct {
		path string
		data []byte
	}{{"/sys/pre-img.bin", original}, {"/cert/c2.der", ca}, {"/sys/mcuimg.bin", preloader}} {
		writes = append(writes, Write{item.path, Digest(item.data), item.data})
	}
	return writes, nil
}
