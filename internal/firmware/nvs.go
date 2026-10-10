// SPDX-License-Identifier: GPL-2.0-only
package firmware

import (
	"bytes"
	"errors"
	"fmt"
)

func nvsCRC(data []byte) uint32 {
	crc := uint32(0)
	for _, b := range data {
		crc ^= uint32(b)
		for range 8 {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0xedb88320
			} else {
				crc >>= 1
			}
		}
	}
	return ^crc
}
func itemCRC(item []byte) uint32 {
	b := append([]byte(nil), item[:4]...)
	b = append(b, item[8:32]...)
	return nvsCRC(b)
}
func nvsState(page []byte, entry int) byte { return page[32+entry/4] >> uint(entry%4*2) & 3 }
func setNVSState(page []byte, entry int, state byte) {
	pos := 32 + entry/4
	shift := uint(entry % 4 * 2)
	page[pos] = page[pos]&^(3<<shift) | state<<shift
}
func nvsPages(data []byte) ([][]byte, error) {
	if len(data)%4096 != 0 {
		return nil, ErrImage
	}
	pages := [][]byte{}
	for offset := 0; offset < len(data); offset += 4096 {
		page := data[offset : offset+4096]
		switch le.Uint32(page) {
		case 0xffffffff, 0xfffffff0:
			continue
		case 0xfffffffe, 0xfffffffc, 0xfffffff8:
			pages = append(pages, page)
		default:
			return nil, fmt.Errorf("%w: NVS page state", ErrImage)
		}
	}
	return pages, nil
}
func fixNVS(data []byte, repair bool) error {
	pages, err := nvsPages(data)
	if err != nil {
		return err
	}
	for _, page := range pages {
		for entry := 0; entry < 126; {
			if nvsState(page, entry) != 2 {
				entry++
				continue
			}
			item := page[64+entry*32 : 96+entry*32]
			span := int(item[2])
			if span < 1 || entry+span > 126 {
				return fmt.Errorf("%w: NVS item span", ErrImage)
			}
			if item[1] == 0x21 || item[1] == 0x42 {
				size := int(le.Uint16(item[24:]))
				if size > 64 || size > (span-1)*32 {
					return fmt.Errorf("%w: NVS value bounds", ErrImage)
				}
				crc := nvsCRC(page[96+entry*32 : 96+entry*32+size])
				if !repair && le.Uint32(item[28:]) != crc {
					return fmt.Errorf("%w: NVS data CRC", ErrImage)
				}
				if repair {
					le.PutUint32(item[28:], crc)
				}
			}
			crc := itemCRC(item)
			if !repair && le.Uint32(item[4:]) != crc {
				return fmt.Errorf("%w: NVS item CRC", ErrImage)
			}
			if repair {
				le.PutUint32(item[4:], crc)
			}
			entry += span
		}
	}
	return nil
}
func nvsKey(item []byte) string { return string(bytes.TrimRight(item[8:24], "\x00")) }
func blobItems(key, value string, namespace byte) []byte {
	span := 2 + len(value)/32
	items := bytes.Repeat([]byte{255}, (span+1)*32)
	head := items[:32]
	head[0] = namespace
	head[1] = 0x42
	head[2] = byte(span)
	head[3] = 0
	clear(head[8:24])
	copy(head[8:], key)
	le.PutUint16(head[24:], uint16(len(value)))
	copy(items[32:], value)
	index := items[span*32:]
	index[0] = namespace
	index[1] = 0x48
	index[2] = 1
	clear(index[8:24])
	copy(index[8:], key)
	le.PutUint32(index[24:], uint32(len(value)))
	index[28] = 1
	index[29] = 0
	return items
}
func addNVS(pages [][]byte, items []byte) error {
	slots := len(items) / 32
	for _, page := range pages {
		for start := 0; start+slots <= 126; start++ {
			free := true
			for i := 0; i < slots; i++ {
				if nvsState(page, start+i) == 2 {
					free = false
					break
				}
			}
			if !free {
				continue
			}
			copy(page[64+start*32:], items)
			for i := 0; i < slots; i++ {
				setNVSState(page, start+i, 2)
			}
			return nil
		}
	}
	return errors.New("NVS capacity exhausted")
}
func (image *Image) PatchWiFi(ssid, password string) error {
	if len(ssid) == 0 || len(ssid) > 63 || len(password) > 63 || bytes.IndexByte([]byte(ssid+password), 0) >= 0 {
		return errors.New("invalid WiFi credentials")
	}
	found := false
	for _, part := range image.Partitions {
		if part.Type != 1 || part.Subtype != 2 {
			continue
		}
		found = true
		data := image.Data[part.Offset : part.Offset+part.Size]
		pages, err := nvsPages(data)
		if err != nil {
			return err
		}
		namespace := byte(0)
		for _, page := range pages {
			for entry := 0; entry < 126; {
				if nvsState(page, entry) != 2 {
					entry++
					continue
				}
				item := page[64+entry*32 : 96+entry*32]
				span := int(item[2])
				if span < 1 || entry+span > 126 {
					return ErrImage
				}
				if item[0] == 0 && nvsKey(item) == "TB_WIFI" {
					namespace = item[24]
				}
				entry += span
			}
		}
		if namespace == 0 {
			return errors.New("TB_WIFI namespace missing")
		}
		for _, page := range pages {
			for entry := 0; entry < 126; {
				if nvsState(page, entry) != 2 {
					entry++
					continue
				}
				item := page[64+entry*32 : 96+entry*32]
				span := int(item[2])
				key := nvsKey(item)
				if item[0] == namespace && (key == "SSID0" || key == "PW0" || key == "INDEX") {
					for i := 0; i < span; i++ {
						setNVSState(page, entry+i, 3)
						copy(page[64+(entry+i)*32:], bytes.Repeat([]byte{255}, 32))
					}
				}
				entry += span
			}
		}
		for _, pair := range [][2]string{{"SSID0", ssid}, {"PW0", password}} {
			if err := addNVS(pages, blobItems(pair[0], pair[1], namespace)); err != nil {
				return err
			}
		}
		index := bytes.Repeat([]byte{255}, 32)
		index[0] = namespace
		index[1] = 1
		index[2] = 1
		clear(index[8:24])
		copy(index[8:], "INDEX")
		index[24] = 1
		if err := addNVS(pages, index); err != nil {
			return err
		}
		if err := fixNVS(data, true); err != nil {
			return err
		}
	}
	if !found {
		return errors.New("NVS partition missing")
	}
	return nil
}
