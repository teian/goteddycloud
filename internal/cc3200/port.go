// SPDX-License-Identifier: GPL-2.0-only
// Patterns and Thumb encoding follow hackiebox_cfw_ng sd-bootloader-ng/tools/altport.py.
package cc3200

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type portPatchEntry struct {
	Description string   `json:"_desc"`
	Search      []string `json:"search"`
	Replace     []string `json:"replace"`
}

// PortPatch creates an SD bootloader patch for CC3200 firmware 3.1.0 BF2,
// 3.3.0 and 3.4.0. It changes API and RTNL connections, preserving TLS.
// The SD bootloader applies this to the original image at boot, not to flash.
func PortPatch(port uint32) ([]byte, error) {
	if port < 1 || port > 65535 {
		return nil, errors.New("port must be 1..65535")
	}
	entries := []portPatchEntry{
		{Description: "connect 3.1.0 BF2: movw r3, #htons(port)", Search: strings.Fields("02 23 ad f8 10 30 ?? ?? 4b f6 01 33 ad f8 0c 30 ad f8 12 30")},
		{Description: "connect 3.3.0/3.4.0: sockaddr literal AF_INET:port", Search: strings.Fields("02 00 01 bb")},
		{Description: "log BF2/3.3.0/3.4.0: connect failed, movw r3, #port", Search: strings.Fields("8b 48 87 4a 40 f2 bb 13 ?? ?? ?? ?? 89 49 b4 f9 00 00")},
		{Description: "log BF2/3.3.0/3.4.0: cloud request, movw sl, #port", Search: strings.Fields("40 f2 bb 1a 02 46 cd f8 04 a0 40 f2 11 30 cd f8 00 80 ?? ?? ?? ?? 1c 22 00 21 11 a8 ?? ?? ?? ?? ?? 4a 4e f6 60 23")},
	}
	for i := range entries {
		entries[i].Replace = make([]string, len(entries[i].Search))
		for j := range entries[i].Replace {
			entries[i].Replace[j] = "??"
		}
	}
	networkPort := uint16(port)<<8 | uint16(port)>>8
	setPortBytes(entries[0].Replace[8:], thumbMOVW(3, networkPort))
	setPortBytes(entries[1].Replace[2:], []byte{byte(port >> 8), byte(port)})
	setPortBytes(entries[2].Replace[4:], thumbMOVW(3, uint16(port)))
	setPortBytes(entries[3].Replace, thumbMOVW(10, uint16(port)))
	patch := struct {
		General struct {
			Description     string `json:"_desc"`
			MemoryPosition  string `json:"_memPos"`
			FirmwareVersion string `json:"_fwVer"`
		} `json:"general"`
		Entries []portPatchEntry `json:"searchAndReplace"`
	}{Entries: entries}
	patch.General.Description = fmt.Sprintf("Changes the port the box connects to (API and RTNL) from 443 to %d.", port)
	patch.General.FirmwareVersion = "3.1.0+"
	return json.MarshalIndent(patch, "", "    ")
}

func setPortBytes(dst []string, code []byte) {
	for i, b := range code {
		dst[i] = fmt.Sprintf("%02x", b)
	}
}

// thumbMOVW encodes a Thumb-2 immediate load into the requested register.
func thumbMOVW(register byte, value uint16) []byte {
	first := uint16(0xf240) | ((value>>11)&1)<<10 | value>>12
	second := ((value>>8)&7)<<12 | uint16(register)<<8 | value&255
	return []byte{byte(first), byte(first >> 8), byte(second), byte(second >> 8)}
}
