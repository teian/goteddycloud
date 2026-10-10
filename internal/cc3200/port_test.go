// SPDX-License-Identifier: GPL-2.0-only
package cc3200

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestPortPatchUpstreamVector(t *testing.T) {
	// Published HackieboxNG hbng_bl_v0.9.3d SD patch, port 10443.
	want, err := os.ReadFile("testdata/altPort.10443.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := PortPatch(10443)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if err := json.Unmarshal(want, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("patch differs from published upstream vector: %s", got)
	}
}

func TestPortPatchEncoding(t *testing.T) {
	// Decode Thumb MOVW independently, checking every representable port and both registers.
	for port := uint32(1); port <= 65535; port++ {
		for _, register := range []byte{3, 10} {
			b := thumbMOVW(register, uint16(port))
			first, second := uint16(b[0])|uint16(b[1])<<8, uint16(b[2])|uint16(b[3])<<8
			decoded := (first&15)<<12 | (first>>10&1)<<11 | (second>>12&7)<<8 | second&255
			if decoded != uint16(port) || byte(second>>8&15) != register {
				t.Fatal(port, register, b)
			}
		}
	}
	for _, port := range []uint32{1, 443, 32768, 65535} {
		if _, err := PortPatch(port); err != nil {
			t.Fatal(port, err)
		}
	}
	for _, port := range []uint32{0, 65536, ^uint32(0)} {
		if _, err := PortPatch(port); err == nil {
			t.Fatal("accepted invalid port", port)
		}
	}
}
