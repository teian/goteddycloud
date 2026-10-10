// SPDX-License-Identifier: GPL-2.0-only
// ESP32 layouts and repair rules follow teddyCloud src/esp32.c; see NOTICE.
package firmware

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

const MaxImage = 64 << 20

var le = binary.LittleEndian
var ErrImage = errors.New("invalid ESP32 image")

type Partition struct {
	Type, Subtype byte
	Offset, Size  int
	Label         string
}
type Image struct {
	Data       []byte
	Partitions []Partition
}

func Parse(data []byte) (*Image, error)  { return parse(data, false) }
func Repair(data []byte) (*Image, error) { return parse(data, true) }
func parse(data []byte, repair bool) (*Image, error) {
	if len(data) < 0x9020 || len(data) > MaxImage {
		return nil, ErrImage
	}
	image := &Image{Data: append([]byte(nil), data...)}
	if err := image.fixApplication(0, len(data), repair); err != nil {
		return nil, err
	}
	terminated := false
	for offset := 0x9000; offset+32 <= len(data) && offset < 0xa000; offset += 32 {
		entry := data[offset : offset+32]
		magic := le.Uint16(entry)
		if magic == 0xebeb {
			sum := md5.Sum(data[0x9000:offset])
			if !bytes.Equal(sum[:], entry[16:32]) {
				return nil, fmt.Errorf("%w: partition digest", ErrImage)
			}
			terminated = true
			break
		}
		if magic == 0xffff {
			terminated = true
			break
		}
		if magic != 0x50aa {
			return nil, fmt.Errorf("%w: partition marker", ErrImage)
		}
		start, size := uint64(le.Uint32(entry[4:])), uint64(le.Uint32(entry[8:]))
		if size == 0 || start < 0xa000 || start+size > uint64(len(data)) {
			return nil, fmt.Errorf("%w: partition bounds", ErrImage)
		}
		part := Partition{Type: entry[2], Subtype: entry[3], Offset: int(start), Size: int(size), Label: string(bytes.TrimRight(entry[12:28], "\x00"))}
		for _, previous := range image.Partitions {
			if part.Offset < previous.Offset+previous.Size && previous.Offset < part.Offset+part.Size {
				return nil, fmt.Errorf("%w: overlapping partitions", ErrImage)
			}
		}
		image.Partitions = append(image.Partitions, part)
		if part.Type == 1 && part.Subtype == 2 {
			if err := fixNVS(image.Data[part.Offset:part.Offset+part.Size], repair); err != nil {
				return nil, err
			}
		}
		if part.Type == 0 {
			if err := image.fixApplication(part.Offset, part.Size, repair); err != nil {
				return nil, err
			}
		}
	}
	if !terminated || len(image.Partitions) == 0 {
		return nil, fmt.Errorf("%w: partition table", ErrImage)
	}
	return image, nil
}
func (image *Image) fixApplication(offset, length int, repair bool) error {
	if offset < 0 || length < 24 || offset > len(image.Data)-length {
		return ErrImage
	}
	data := image.Data[offset : offset+length]
	if data[0] != 0xe9 || data[1] == 0 || data[1] > 16 || data[23] > 1 {
		return fmt.Errorf("%w: application header", ErrImage)
	}
	pos := 24
	checksum := byte(0xef)
	for range int(data[1]) {
		if pos > len(data)-8 {
			return ErrImage
		}
		size := uint64(le.Uint32(data[pos+4:]))
		pos += 8
		if size > uint64(len(data)-pos) {
			return ErrImage
		}
		for _, value := range data[pos : pos+int(size)] {
			checksum ^= value
		}
		pos += int(size)
	}
	footer := pos | 15
	if footer >= len(data) {
		return ErrImage
	}
	// The C hash calculation treats alignment bytes as zero. Reject nonzero
	// padding rather than repairing a digest over different bytes silently.
	for _, b := range data[pos:footer] {
		if b != 0 {
			return fmt.Errorf("%w: application padding", ErrImage)
		}
	}
	if !repair && data[footer] != checksum {
		return fmt.Errorf("%w: application checksum", ErrImage)
	}
	if repair {
		data[footer] = checksum
	}
	if data[23] == 1 {
		if footer+33 > len(data) {
			return ErrImage
		}
		digest := sha256.Sum256(data[:footer+1])
		if !repair && !bytes.Equal(digest[:], data[footer+1:footer+33]) {
			return fmt.Errorf("%w: application digest", ErrImage)
		}
		if repair {
			copy(data[footer+1:], digest[:])
		}
	}
	return nil
}
func (image *Image) Fixup() error {
	if err := image.fixApplication(0, len(image.Data), true); err != nil {
		return err
	}
	for _, part := range image.Partitions {
		if part.Type == 1 && part.Subtype == 2 {
			if err := fixNVS(image.Data[part.Offset:part.Offset+part.Size], true); err != nil {
				return err
			}
		}
		if part.Type == 0 {
			if err := image.fixApplication(part.Offset, part.Size, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// PatchHost only replaces complete C strings that fit in their original slot.
// The C implementation permits overwriting following bytes for longer names;
// this implementation rejects that corruption-prone case.
func (image *Image) PatchHost(host, oldRTNL, oldAPI string) (int, error) {
	if host == "" || len(host) > 31 || bytes.IndexByte([]byte(host), 0) >= 0 {
		return 0, errors.New("invalid replacement hostname")
	}
	patterns := []string{oldRTNL}
	if oldAPI != oldRTNL {
		patterns = append(patterns, oldAPI)
	}
	count := 0
	for _, old := range patterns {
		if old == "" {
			return 0, errors.New("empty old hostname")
		}
		pattern := append([]byte(old), 0)
		for offset := 0; offset <= len(image.Data)-len(pattern); {
			index := bytes.Index(image.Data[offset:], pattern)
			if index < 0 {
				break
			}
			index += offset
			if len(host) > len(old) {
				return 0, errors.New("replacement hostname exceeds existing slot")
			}
			copy(image.Data[index:], append([]byte(host), 0))
			offset = index + len(pattern)
			count++
		}
	}
	return count, nil
}
func (image *Image) Assets() (*FAT, error) {
	for _, part := range image.Partitions {
		if part.Label == "assets" && part.Type == 1 && part.Subtype == 0x81 {
			return openFAT(image.Data[part.Offset : part.Offset+part.Size])
		}
	}
	return nil, errors.New("assets partition not found")
}
