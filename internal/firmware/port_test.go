// SPDX-License-Identifier: GPL-2.0-only
package firmware

import (
	"bytes"
	"testing"
)

func connectFixture() []byte {
	b := make([]byte, 0xd2+len(connectTail))
	for i, v := range connectStart {
		if v >= 0 {
			b[i] = byte(v)
		}
	}
	for i, v := range connectTail {
		if v >= 0 {
			b[0xd2+i] = byte(v)
		}
	}
	for _, s := range connectSites {
		copy(b[s.offset:], s.original[:])
	}
	return b
}
func TestPortPatchRestorationBoundsAndChecksum(t *testing.T) {
	original := connectFixture()
	data := syntheticImage(t)
	copy(data[0xa000:], application(original))
	image, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), image.Data...)
	for _, port := range []uint32{1, 8443, 32767, 443} {
		count, err := image.PatchPort(port)
		if err != nil || count != 1 {
			t.Fatal(port, count, err)
		}
		if _, err := Parse(image.Data); err != nil {
			t.Fatal(err)
		}
		if port != 443 {
			f := image.Data[0xa020:]
			if f[0x14] != byte(port) || f[0xd4] != byte(port>>8) || !bytes.Equal(f[0xe3:0xe6], []byte{0x52, 0xa0, 0}) {
				t.Fatal("wrong instruction bytes")
			}
		}
	}
	if !bytes.Equal(before, image.Data) {
		t.Fatal("443 failed to restore original image")
	}
	for _, port := range []uint32{0, 32768, 65536} {
		if _, err := image.PatchPort(port); err == nil {
			t.Fatal(port)
		}
	}
	for n := 0; n < len(original); n++ {
		if patchConnectionPorts(append([]byte(nil), original[:n]...), 8443) != 0 {
			t.Fatal("matched truncated function", n)
		}
	}
}

func TestPortPatchAllCopiesAndUnsupportedPreserved(t *testing.T) {
	data := syntheticImage(t)
	code := append(connectFixture(), connectFixture()...)
	copy(data[0xa000:], application(code))
	image, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), image.Data...)
	if count, err := image.PatchPort(10443); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	if count, err := image.PatchPort(443); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	if !bytes.Equal(before, image.Data) {
		t.Fatal("restoration changed image")
	}
	image, err = Parse(syntheticImage(t))
	if err != nil {
		t.Fatal(err)
	}
	before = append([]byte(nil), image.Data...)
	if _, err := image.PatchPort(10443); err == nil {
		t.Fatal("accepted unsupported firmware")
	}
	if !bytes.Equal(before, image.Data) {
		t.Fatal("failed patch modified image")
	}
}
