// SPDX-License-Identifier: GPL-2.0-only
package cc3200

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func namedImage(cc32xx bool) []byte {
	image := testFlash()
	image[7] = 0xc0 // one unmirrored block
	offset, nameOffset := 0x774, 0x200
	if cc32xx {
		offset, nameOffset = 0x2000, 0x3c0
	}
	name := "/cert/ca.der"
	binary.LittleEndian.PutUint16(image[offset:], 0)
	binary.LittleEndian.PutUint16(image[offset+2:], uint16(len(name)))
	copy(image[offset+nameOffset:], name)
	start := 5 * BlockSize
	copy(image[start:], []byte{4, 0, 0, 0, 0, 0, 0, 0})
	copy(image[start+8:], []byte{1, 2, 3, 4})
	return image
}
func TestOfflineImageReadAndReplacement(t *testing.T) {
	for _, cc32xx := range []bool{false, true} {
		image := namedImage(cc32xx)
		before := append([]byte(nil), image...)
		options := ImageOptions{CC32XX: cc32xx}
		for _, id := range []int{-1, 0} {
			data, err := ImageFile(image, options, "/cert/ca.der", id)
			if err != nil || !bytes.Equal(data, []byte{1, 2, 3, 4}) {
				t.Fatal(data, err)
			}
		}
		updated, err := ReplaceImageFile(image, options, "/cert/ca.der", -1, []byte{8, 9})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(image, before) {
			t.Fatal("input image mutated")
		}
		data, err := ImageFile(updated, options, "/cert/ca.der", -1)
		if err != nil || !bytes.Equal(data, []byte{8, 9}) {
			t.Fatal(data, err)
		}
		if _, err = ReplaceImageFile(image, options, "/cert/ca.der", -1, []byte{1, 2, 3, 4, 5}); err == nil {
			t.Fatal("expanded file")
		}
	}
}
func TestOfflineImageHeaderBoundsAndMirrors(t *testing.T) {
	image := namedImage(false)
	image[5*BlockSize+2] = 1
	if _, err := ImageFile(image, ImageOptions{}, "/cert/ca.der", -1); err == nil {
		t.Fatal("accepted oversized file header")
	}
	image = namedImage(false)
	image[7] = 0x80
	if _, err := ReplaceImageFile(image, ImageOptions{}, "/cert/ca.der", -1, []byte{1}); err == nil {
		t.Fatal("raw patched mirrored file")
	}
	if _, err := InspectImage(image, ImageOptions{Inactive: true}); err == nil {
		t.Fatal("accepted missing inactive FAT")
	}
}
func TestOfflineInactiveFATUsesOwnMetadata(t *testing.T) {
	image := namedImage(false)
	copy(image[BlockSize:2*BlockSize], image[:BlockSize])
	binary.LittleEndian.PutUint16(image[BlockSize:], 2)
	metadata := BlockSize + 0x774
	binary.LittleEndian.PutUint16(image[metadata:], 0)
	binary.LittleEndian.PutUint16(image[metadata+2:], uint16(len("/cert/other.der")))
	copy(image[metadata+0x200:], "/cert/other.der")
	active, err := InspectImage(image, ImageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	inactive, err := InspectImage(image, ImageOptions{Inactive: true})
	if err != nil {
		t.Fatal(err)
	}
	if active.Entries[0].Name != "/cert/other.der" || inactive.Entries[0].Name != "/cert/ca.der" {
		t.Fatal("FAT names mixed", active, inactive)
	}
}
