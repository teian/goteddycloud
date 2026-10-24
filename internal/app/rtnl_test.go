// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"goteddycloud/internal/boxstate"
	"goteddycloud/internal/content"
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

func ingestRecognitionRTNL(t *testing.T, s *Server, box string, group, function uint32, payload []byte) {
	t.Helper()
	wire, err := proto.Marshal(&protocol.TonieRtnlRPC{Log2: &protocol.TonieRtnlLog2{
		Uptime: proto.Uint64(1), Sequence: proto.Uint32(1), Field3: proto.Uint32(0),
		FunctionGroup: proto.Uint32(group), Function: proto.Uint32(function), Field6: payload,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Boxes.Ingest(box, wire); err != nil {
		t.Fatal(err)
	}
}

func presentRecognitionRTNL(t *testing.T, s *Server, box, ruid string) {
	t.Helper()
	uid, err := content.UIDFromRUID(ruid)
	if err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 8)
	binary.BigEndian.PutUint32(payload, uint32(uid))
	binary.BigEndian.PutUint32(payload[4:], uint32(uid>>32))
	ingestRecognitionRTNL(t, s, box, boxstate.RTNL2_FUGR_TAG, boxstate.RTNL2_FUNC_TAG_VALID_CC3200, payload)
}

func TestRTNLAudioRecognizesAndPersistsTagWithoutFreshnessOrDownload(t *testing.T) {
	for _, event := range []struct{ group, function uint32 }{
		{boxstate.RTNL2_FUGR_AUDIO_A, boxstate.RTNL2_FUNC_AUDIO_ID_CC3200},
		{boxstate.RTNL2_FUGR_AUDIO_A, boxstate.RTNL2_FUNC_AUDIO_ID_ESP32},
		{boxstate.RTNL2_FUGR_AUDIO_B, boxstate.RTNL2_FUNC_AUDIO_ID},
	} {
		for _, audioID := range []uint32{0x60000011, 0x10000011} {
			t.Run(fmt.Sprintf("%d/%d/%x", event.group, event.function, audioID), func(t *testing.T) {
				s := fixtureApp(t)
				if err := s.Catalog.ReplaceOfficial([]byte(recognitionCatalog)); err != nil {
					t.Fatal(err)
				}
				const id, ruid = "001122334455", "11000000500304e0"
				if err := s.Settings.Set(id, "core.contentdir", "rtnl-recognition"); err != nil {
					t.Fatal(err)
				}
				snap := s.Settings.Snapshot(id)
				if err := os.MkdirAll(snap.String("internal.contentdirfull"), 0700); err != nil {
					t.Fatal(err)
				}
				presentRecognitionRTNL(t, s, id, ruid)
				stream, unsubscribe, ok := s.Events.Subscribe()
				if !ok {
					t.Fatal("subscribe")
				}
				defer unsubscribe()
				payload := make([]byte, 4)
				binary.LittleEndian.PutUint32(payload, audioID)
				ingestRecognitionRTNL(t, s, id, event.group, event.function, payload)
				// Recognition must already be durable before any browser/API read.
				m, err := s.Content.Metadata(snap, ruid)
				if err != nil || m.String("tonie_model") != "AUTO_TEST" || boxAudioID(m) != audioID {
					t.Fatal("RTNL recognition was not persisted", m.String("tonie_model"), boxAudioID(m), err)
				}
				updated := false
				for len(stream) > 0 {
					event := <-stream
					updated = updated || event.Name == "tag-updated" && event.Box == id
				}
				if !updated {
					t.Fatal("recognized tag did not notify the UI")
				}
				info, err := s.tagInfo(context.Background(), snap, id, ruid)
				if err != nil || info["exists"] != false || info["tonieInfo"].(map[string]any)["model"] != "AUTO_TEST" {
					t.Fatal("recognized tag not visible without local audio", info, err)
				}
				global, err := s.Content.Metadata(s.Settings.Snapshot(""), ruid)
				if err != nil || global.String("tonie_model") != "" || boxAudioID(global) != 0 {
					t.Fatal("RTNL recognition leaked into global content", err)
				}
			})
		}
	}
}

func TestRTNLRetainsUnknownAudioForLaterCatalogAndPreservesManualModels(t *testing.T) {
	for _, manual := range []string{"", "MANUAL_MODEL"} {
		t.Run(manual, func(t *testing.T) {
			s := fixtureApp(t)
			const id, ruid = "001122334455", "11000000500304e0"
			snap := s.Settings.Snapshot(id)
			if err := s.Content.Update(snap, ruid, func(m content.Metadata) error {
				m.Set("tonie_model", manual)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			presentRecognitionRTNL(t, s, id, ruid)
			payload := make([]byte, 4)
			binary.LittleEndian.PutUint32(payload, 0x60000011)
			ingestRecognitionRTNL(t, s, id, boxstate.RTNL2_FUGR_AUDIO_A, boxstate.RTNL2_FUNC_AUDIO_ID_CC3200, payload)
			m, err := s.Content.Metadata(snap, ruid)
			if err != nil || boxAudioID(m) != 0x60000011 || m.String("tonie_model") != manual {
				t.Fatal("RTNL lost unknown ID or changed a manual model", m, err)
			}
			if err := s.Catalog.ReplaceOfficial([]byte(recognitionCatalog)); err != nil {
				t.Fatal(err)
			}
			info, err := s.tagInfo(context.Background(), snap, id, ruid)
			want := manual
			if want == "" {
				want = "AUTO_TEST"
			}
			if err != nil || info["tonieInfo"].(map[string]any)["model"] != want {
				t.Fatal("late RTNL recognition failed", info, err)
			}
		})
	}
}

func TestRTNLAudioWithoutTagAndTagChangeDoNotAssignPreviousContent(t *testing.T) {
	s := fixtureApp(t)
	if err := s.Catalog.ReplaceOfficial([]byte(recognitionCatalog)); err != nil {
		t.Fatal(err)
	}
	const id, first, second = "001122334455", "11000000500304e0", "22000000500304e0"
	snap := s.Settings.Snapshot(id)
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, 0x60000011)
	ingestRecognitionRTNL(t, s, id, boxstate.RTNL2_FUGR_AUDIO_A, boxstate.RTNL2_FUNC_AUDIO_ID_CC3200, payload)
	root, _, err := s.Content.ManagedRoot(snap, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.Stat("00000000/00000000.json"); !os.IsNotExist(err) {
		t.Fatal("audio without a tag created metadata", err)
	}
	presentRecognitionRTNL(t, s, id, first)
	ingestRecognitionRTNL(t, s, id, boxstate.RTNL2_FUGR_AUDIO_A, boxstate.RTNL2_FUNC_AUDIO_ID_CC3200, payload)
	presentRecognitionRTNL(t, s, id, second)
	m, err := s.Content.Metadata(snap, second)
	if err != nil || m.String("tonie_model") != "" || boxAudioID(m) != 0 {
		t.Fatal("new tag inherited the previous audio", m, err)
	}
}
