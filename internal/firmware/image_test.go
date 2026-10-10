// SPDX-License-Identifier: GPL-2.0-only
package firmware

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

func application(payload []byte) []byte {
	footer := (24 + 8 + len(payload)) | 15
	b := make([]byte, footer+33)
	b[0] = 0xe9
	b[1] = 1
	b[23] = 1
	le.PutUint32(b[28:], uint32(len(payload)))
	copy(b[32:], payload)
	checksum := byte(0xef)
	for _, v := range payload {
		checksum ^= v
	}
	b[footer] = checksum
	digest := sha256.Sum256(b[:footer+1])
	copy(b[footer+1:], digest[:])
	return b
}
func syntheticImage(t testing.TB) []byte {
	t.Helper()
	data := bytes.Repeat([]byte{255}, 0x110000)
	copy(data, application([]byte("boot\x00")))
	copy(data[0xa000:], application([]byte("rtnl.bxcl.de\x00prod.de.tbs.toys\x00")))
	for i, p := range []Partition{{Type: 0, Offset: 0xa000, Size: 0x2000, Label: "app"}, {Type: 1, Subtype: 0x81, Offset: 0xc000, Size: 0x100000, Label: "assets"}} {
		b := data[0x9000+i*32 : 0x9020+i*32]
		clear(b)
		le.PutUint16(b, 0x50aa)
		b[2] = p.Type
		b[3] = p.Subtype
		le.PutUint32(b[4:], uint32(p.Offset))
		le.PutUint32(b[8:], uint32(p.Size))
		copy(b[12:], p.Label)
	}
	partition := data[0xc000:0x10c000]
	logical := make([]byte, 250*4096)
	b := logical
	b[0] = 0xeb
	le.PutUint16(b[11:], 4096)
	b[13] = 1
	le.PutUint16(b[14:], 1)
	b[16] = 1
	le.PutUint16(b[17:], 128)
	le.PutUint16(b[19:], 250)
	b[21] = 0xf8
	le.PutUint16(b[22:], 1)
	b[510] = 0x55
	b[511] = 0xaa
	copy(b[4096:], []byte{0xf8, 0xff, 0xff})
	b[4096+3] = 0xff
	b[4096+4] = 0x0f // cluster 2 EOC
	copy(b[8192:], []byte("CERT       "))
	b[8192+11] = 16
	le.PutUint16(b[8192+26:], 2)
	for i := range 250 {
		copy(partition[(i+1)*4096:], logical[i*4096:(i+1)*4096])
	}
	state := partition[251*4096:]
	clear(state[:60])
	le.PutUint32(state[4:], 251)
	le.PutUint32(state[20:], 4096)
	return data
}
func TestESPImageFATRoundtripAndOriginalPreservation(t *testing.T) {
	original := syntheticImage(t)
	copyBefore := append([]byte(nil), original...)
	image, err := Parse(original)
	if err != nil {
		t.Fatal(err)
	}
	fat, err := image.Assets()
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"CA.DER": "synthetic CA", "CLIENT.DER": "synthetic client", "PRIVATE.DER": "synthetic key"} {
		if err := fat.Put("CERT", name, []byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	fat.Commit()
	reread, err := Parse(image.Data)
	if err != nil {
		t.Fatal(err)
	}
	fat, err = reread.Assets()
	if err != nil {
		t.Fatal(err)
	}
	files, err := fat.Extract("CERT")
	if err != nil || string(files["CA.DER"]) != "synthetic CA" || len(files) != 3 {
		t.Fatal(files, err)
	}
	if err := fat.Put("CERT", "ca.der", bytes.Repeat([]byte{'a'}, 5000)); err != nil {
		t.Fatal(err)
	}
	fat.Commit()
	files, err = fat.Extract("CERT")
	if err != nil || len(files["CA.DER"]) != 5000 {
		t.Fatal(files, err)
	}
	count, err := reread.PatchHost("tc.local", "rtnl.bxcl.de", "prod.de.tbs.toys")
	if err != nil || count != 2 {
		t.Fatal(count, err)
	}
	if err := reread.Fixup(); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(reread.Data); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, copyBefore) {
		t.Fatal("caller image modified")
	}
}
func TestRejectFirmwareCorruptionAndRepairChecksum(t *testing.T) {
	original := syntheticImage(t)
	bad := append([]byte(nil), original...)
	bad[0xa000+32] ^= 1
	if _, err := Parse(bad); err == nil {
		t.Fatal("bad checksum accepted")
	}
	repaired, err := Repair(bad)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(repaired.Data); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func([]byte){func(b []byte) { le.PutUint32(b[0x9008:], MaxImage) }, func(b []byte) { le.PutUint32(b[0x9024:], 0xa000) }, func(b []byte) { b[0xa001] = 255 }, func(b []byte) { le.PutUint32(b[0xa01c:], 0xffffffff) }} {
		bad = append([]byte(nil), original...)
		mutate(bad)
		if _, err := Parse(bad); err == nil {
			t.Fatal("malformed layout accepted")
		}
	}
}
func FuzzFirmwareParse(f *testing.F) {
	f.Add([]byte{0xe9, 1})
	f.Add(syntheticImage(f))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > MaxImage {
			return
		}
		image, err := Parse(b)
		if err == nil {
			_, _ = image.Assets()
		}
	})
}

func TestNVSWiFiPatchRepairsCRCsAndRejectsMissingNamespace(t *testing.T) {
	data := syntheticImage(t)
	entry := data[0x9040:0x9060]
	clear(entry)
	le.PutUint16(entry, 0x50aa)
	entry[2] = 1
	entry[3] = 2
	le.PutUint32(entry[4:], 0x10c000)
	le.PutUint32(entry[8:], 0x4000)
	copy(entry[12:], "nvs")
	page := data[0x10c000:0x10d000]
	le.PutUint32(page, 0xfffffffe)
	item := page[64:96]
	item[0] = 0
	item[1] = 1
	item[2] = 1
	clear(item[8:24])
	copy(item[8:], "TB_WIFI")
	item[24] = 1
	le.PutUint32(item[4:], itemCRC(item))
	setNVSState(page, 0, 2)
	image, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, ssid := range []string{"first", "replacement"} {
		if err := image.PatchWiFi(ssid, "secret"); err != nil {
			t.Fatal(err)
		}
		if _, err := Parse(image.Data); err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(image.Data, []byte(ssid)) {
			t.Fatal("SSID not written")
		}
	}
	if bytes.Contains(image.Data, []byte("first")) {
		t.Fatal("old WiFi value retained")
	}
	corrupted := append([]byte(nil), image.Data...)
	corrupted[0x10c000+64+32+4] ^= 1
	if _, err := Parse(corrupted); err == nil {
		t.Fatal("NVS CRC ignored")
	}
	if _, err := Repair(corrupted); err != nil {
		t.Fatal(err)
	}
	if err := image.PatchWiFi(string(bytes.Repeat([]byte{'x'}, 64)), ""); err == nil {
		t.Fatal("oversize SSID accepted")
	}
}
