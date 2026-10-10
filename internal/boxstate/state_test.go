// SPDX-License-Identifier: GPL-2.0-only
package boxstate

import (
	"encoding/binary"
	"testing"

	"google.golang.org/protobuf/proto"

	"goteddycloud/internal/events"
	"goteddycloud/internal/protocol"
)

func TestStateIsolationAndMalformedPayloadHasNoEffects(t *testing.T) {
	hub := events.New()
	store := New(hub)
	subscription, close, ok := hub.Subscribe()
	if !ok {
		t.Fatal("subscription")
	}
	defer close()
	payload := make([]byte, 12)
	binary.LittleEndian.PutUint32(payload[4:], 0xffffffd8)
	binary.LittleEndian.PutUint32(payload[8:], 2)
	rpc := &protocol.TonieRtnlRPC{Log2: &protocol.TonieRtnlLog2{Uptime: proto.Uint64(1234), Sequence: proto.Uint32(1), Field3: proto.Uint32(0), FunctionGroup: proto.Uint32(RTNL2_FUGR_VOLUME), Function: proto.Uint32(RTNL2_FUNC_VOLUME_CHANGE_CC3200), Field6: payload}}
	wire, err := proto.Marshal(rpc)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Ingest("one", wire); err != nil {
		t.Fatal(err)
	}
	state := store.Snapshot("one")
	if state.Volume != 2 || state.VolumeDB != -40 || store.Snapshot("two").Volume != 0 {
		t.Fatal(state)
	}
	for _, name := range []string{"rtnl-raw-log2", "VolumeLevel", "VolumedB"} {
		if got := (<-subscription).Name; got != name {
			t.Fatal(got)
		}
	}
	rpc.Log2.Field6 = []byte{0}
	wire, _ = proto.Marshal(rpc)
	if err := store.Ingest("one", wire); err == nil {
		t.Fatal("short payload accepted")
	}
	if len(subscription) != 0 || store.Snapshot("one") != state {
		t.Fatal("malformed state mutation")
	}
	rpc = &protocol.TonieRtnlRPC{Log3: &protocol.TonieRtnlLog3{Datetime: proto.Uint32(1700000000), Field2: proto.Uint32(RTNL3_TYPE_PLAYBACK_STARTING)}}
	wire, _ = proto.Marshal(rpc)
	if err := store.Ingest("two", wire); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"rtnl-raw-log3", "playback", "playback"} {
		if got := (<-subscription).Name; got != name {
			t.Fatal(got)
		}
	}
	if store.Snapshot("two").Playback != "started" || store.Snapshot("one").Playback != "" {
		t.Fatal("playback isolation")
	}
}

func TestCombinedLogsPreserveReferenceEventOrder(t *testing.T) {
	hub := events.New()
	store := New(hub)
	subscription, close, ok := hub.Subscribe()
	if !ok {
		t.Fatal("subscription")
	}
	defer close()
	store.ContentInfo = func(box string, uid uint64, audioID uint32) (string, string) {
		return "Synthetic title", "/synthetic.png"
	}
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, 123)
	rpc := &protocol.TonieRtnlRPC{
		Log2: &protocol.TonieRtnlLog2{Uptime: proto.Uint64(1234), Sequence: proto.Uint32(1), Field3: proto.Uint32(0), FunctionGroup: proto.Uint32(RTNL2_FUGR_AUDIO_A), Function: proto.Uint32(RTNL2_FUNC_AUDIO_ID_CC3200), Field6: payload},
		Log3: &protocol.TonieRtnlLog3{Datetime: proto.Uint32(1700000000), Field2: proto.Uint32(RTNL3_TYPE_PLAYBACK_STOPPED)},
	}
	wire, err := proto.Marshal(rpc)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Ingest("one", wire); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"rtnl-raw-log2", "rtnl-raw-log3", "playback", "ContentAudioId", "ContentTitle", "ContentPicture"} {
		if got := (<-subscription).Name; got != name {
			t.Fatalf("got %s want %s", got, name)
		}
	}
	if store.Snapshot("one").AudioID != 123 {
		t.Fatal("log3 ran after log2")
	}
}

func TestSystemSoundPlaybackRequiresAudioBAndExactPathComponents(t *testing.T) {
	for _, test := range []struct {
		group uint32
		path  string
		stops bool
	}{
		{RTNL2_FUGR_AUDIO_A, "content/00000001/00000001", false},
		{RTNL2_FUGR_AUDIO_B, "content/00000001/00000001", true},
		{RTNL2_FUGR_AUDIO_B, "/content//00000001/00000001", true},
		{RTNL2_FUGR_AUDIO_B, "content/000000001/00000001", false},
		{RTNL2_FUGR_AUDIO_B, "content/00000001/000000001", false},
		{RTNL2_FUGR_AUDIO_B, "/00000001/00000001", false},
	} {
		store := New(events.New())
		rpc := &protocol.TonieRtnlRPC{Log2: &protocol.TonieRtnlLog2{Uptime: proto.Uint64(0), Sequence: proto.Uint32(0), Field3: proto.Uint32(0), FunctionGroup: proto.Uint32(test.group), Function: proto.Uint32(RTNL2_FUNC_AUDIO_PLAY), Field6: append([]byte(test.path), 0)}}
		data, err := proto.Marshal(rpc)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Ingest("box", data); err != nil {
			t.Fatal(err)
		}
		if stopped := store.Snapshot("box").Playback == "stopped"; stopped != test.stops {
			t.Fatalf("%v %q stopped %v", test.group, test.path, stopped)
		}
	}
}
