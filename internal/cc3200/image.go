// SPDX-License-Identifier: GPL-2.0-only
package cc3200

import (
	"errors"
	"fmt"
	"os"
)

// ImageFile reads an existing file by canonical name, or ID when id >= 0.
// File data is bounded by its allocation, including its eight-byte header.
func ImageFile(flash []byte, options ImageOptions, name string, id int) ([]byte, error) {
	entry, err := imageEntry(flash, options, name, id)
	if err != nil {
		return nil, err
	}
	start := entry.Start * BlockSize
	header := flash[start : start+8]
	size := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	if size > entry.Blocks*BlockSize-8 {
		return nil, errors.New("file header exceeds FAT allocation")
	}
	return append([]byte(nil), flash[start+8:start+8+size]...), nil
}
func imageEntry(flash []byte, options ImageOptions, name string, id int) (Entry, error) {
	manifest, err := InspectImage(flash, options)
	if err != nil {
		return Entry{}, err
	}
	for _, entry := range manifest.Entries {
		if (id >= 0 && entry.ID == id) || (id < 0 && entry.Name == name) {
			return entry, nil
		}
	}
	return Entry{}, fmt.Errorf("device file %q (id %d): %w", name, id, os.ErrNotExist)
}

// ReplaceImageFile returns a new image. It never expands a file or modifies
// secure/mirrored files, whose signatures/commit semantics require the ROM API.
func ReplaceImageFile(flash []byte, options ImageOptions, name string, id int, data []byte) ([]byte, error) {
	entry, err := imageEntry(flash, options, name, id)
	if err != nil {
		return nil, err
	}
	if entry.Mirrored {
		return nil, errors.New("raw replacement of mirrored files is unsupported; use the device file API")
	}
	previous, err := ImageFile(flash, options, name, id)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > len(previous) {
		return nil, errors.New("replacement must not exceed the previous file length")
	}
	result := append([]byte(nil), flash...)
	start := entry.Start * BlockSize
	result[start] = byte(len(data))
	result[start+1] = byte(len(data) >> 8)
	result[start+2] = byte(len(data) >> 16)
	copy(result[start+8:], data)
	return result, nil
}
