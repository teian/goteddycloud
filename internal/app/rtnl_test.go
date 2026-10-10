// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"goteddycloud/internal/boxstate"
	"goteddycloud/internal/protocol"
)

func TestRTNLLogsAndTransientFirmware(t *testing.T) {
	s := fixtureApp(t)
	s.Now = func() time.Time { return time.Unix(1700000000, 0) }
	id := "001122334455"
	for _, key := range []string{"rtnl.logRaw", "rtnl.logHuman"} {
		if err := s.Settings.Set(id, key, "true"); err != nil {
			t.Fatal(err)
		}
	}
	snap := s.Settings.Snapshot(id)
	configPath := filepath.Join(snap.Base, "config/config.overlay.ini")
	before, _ := os.ReadFile(configPath)
	rpc := &protocol.TonieRtnlRPC{Log2: &protocol.TonieRtnlLog2{Uptime: proto.Uint64(1234), Sequence: proto.Uint32(1), Field3: proto.Uint32(0), FunctionGroup: proto.Uint32(boxstate.RTNL2_FUGR_FIRMWARE), Function: proto.Uint32(boxstate.RTNL2_FUNC_FIRMWARE_VERSION), Field6: []byte("synthetic\x00")}}
	wire, err := proto.Marshal(rpc)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Boxes.Ingest(id, wire); err != nil {
		t.Fatal(err)
	}
	s.updateRTNLSettings(id, wire)
	s.logRTNL(snap, wire)
	s.logRTNL(snap, wire)
	if got := s.Settings.Snapshot(id).String("internal.toniebox_firmware.rtnlVersion"); got != "synthetic" {
		t.Fatal(got)
	}
	if got := s.Settings.Snapshot("AABBCCDDEEFF").String("internal.toniebox_firmware.rtnlVersion"); got != "" {
		t.Fatal("firmware leaked", got)
	}
	after, _ := os.ReadFile(configPath)
	if !bytes.Equal(before, after) {
		t.Fatal("runtime fields persisted")
	}
	raw, err := os.ReadFile(filepath.Join(snap.Base, "config/rtnl.bin"))
	if err != nil {
		t.Fatal(err)
	}
	frame := make([]byte, 4)
	binary.BigEndian.PutUint32(frame, uint32(len(wire)))
	frame = append(frame, wire...)
	if !bytes.Equal(raw, append(append([]byte{}, frame...), frame...)) {
		t.Fatal("raw frame mismatch")
	}
	csv, err := os.ReadFile(filepath.Join(snap.Base, "config/rtnl.csv"))
	if err != nil {
		t.Fatal(err)
	}
	expected := []byte(rtnlCSVHeader)
	expected = append(expected, rtnlCSV(1700000000, rpc)...)
	expected = append(expected, rtnlCSV(1700000000, rpc)...)
	if !bytes.Equal(csv, expected) {
		t.Fatal("CSV mismatch")
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(snap.Base, "config/rtnl.bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(snap.Base, "config/rtnl.bin")); err != nil {
		t.Skip(err)
	}
	s.logRTNL(snap, wire)
	retained, _ := os.ReadFile(outside)
	if string(retained) != "sentinel" {
		t.Fatal("symlink escaped log root")
	}
}

func TestDevelopCSVTextEscaping(t *testing.T) {
	if got := rtnlText([]byte{'a', ' ', 0, '"', '\n', '\r', '\\'}); got != "a..\"\"\\n\\r." {
		t.Fatalf("%q", got)
	}
}

func TestDevelopRTNLEscaping(t *testing.T) {
	if got := rtnlText([]byte{'a', '"', '\n', '\r', '\\', 0, 255, '9'}); got != "a\"\"\\n\\r...9" {
		t.Fatal(got)
	}
}
