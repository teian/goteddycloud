// SPDX-License-Identifier: GPL-2.0-only
// Instruction patterns follow upstream src/esp32_port.c (firmware v5.231.0–v5.237.0).
package firmware

import (
	"bytes"
	"errors"
)

const (
	defaultConnectionPort = 443
	maxConnectionPort     = 32767
	connectTailOffset     = 0xd2
	instructionWildcard   = -1
)

var connectStart = []int16{
	0x36, 0x21, 0x01, 0x72, 0x61, 0x19, 0x62, 0x61, 0x18,
	instructionWildcard, instructionWildcard, instructionWildcard,
	0x20, 0x72, 0x20, 0x61,
	instructionWildcard, instructionWildcard, instructionWildcard, instructionWildcard, instructionWildcard,
	0xa0, 0x2a, 0x20,
}
var connectTail = []int16{
	instructionWildcard, instructionWildcard, instructionWildcard,
	0x0c, 0x15, 0x8a, 0x84, 0x99, 0x21, 0x52, 0x41, 0x34,
	0x92, 0x21, 0x16, 0x0c, 0x15,
	instructionWildcard, instructionWildcard, instructionWildcard,
	0xa9, 0x11, 0xad, 0x03,
}
var connectSites = []struct {
	offset   int
	original [3]byte
	prefix   [2]byte
}{
	{offset: 0x12, original: [3]byte{0x40, 0x40, 0xf4}, prefix: [2]byte{0x42, 0xa0}},
	{offset: connectTailOffset, original: [3]byte{0x82, 0xae, 0x45}, prefix: [2]byte{0x42, 0xd4}},
	{offset: 0xe3, original: [3]byte{0x80, 0x57, 0x83}, prefix: [2]byte{0x52, 0xa0}},
}

func matchesInstructions(data []byte, pattern []int16) bool {
	if len(data) < len(pattern) {
		return false
	}
	for i, v := range pattern {
		if v >= 0 && byte(v) != data[i] {
			return false
		}
	}
	return true
}
func patchConnectionPorts(data []byte, port uint16) int {
	count := 0
	for pos := 0; pos <= len(data)-(connectTailOffset+len(connectTail)); pos++ {
		f := data[pos:]
		if !matchesInstructions(f, connectStart) || !matchesInstructions(f[connectTailOffset:], connectTail) {
			continue
		}
		valid := true
		for _, site := range connectSites {
			if !bytes.Equal(f[site.offset:site.offset+3], site.original[:]) && !bytes.Equal(f[site.offset:site.offset+2], site.prefix[:]) {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		patched := [][3]byte{{0x42, 0xa0, byte(port)}, {0x42, 0xd4, byte(port >> 8)}, {0x52, 0xa0, 0}}
		for i, site := range connectSites {
			code := patched[i]
			if port == defaultConnectionPort {
				code = site.original
			}
			copy(f[site.offset:], code[:])
		}
		count++
	}
	return count
}

// PatchPort publishes changes only after the copied image passes checksum repair.
func (image *Image) PatchPort(port uint32) (int, error) {
	if port < 1 || port > maxConnectionPort {
		return 0, errors.New("port must be 1..32767")
	}
	data := append([]byte(nil), image.Data...)
	count := patchConnectionPorts(data, uint16(port))
	if count == 0 {
		return 0, errors.New("connect function not found; unsupported firmware")
	}
	repaired, err := Repair(data)
	if err != nil {
		return 0, err
	}
	image.Data = repaired.Data
	image.Partitions = repaired.Partitions
	return count, nil
}
