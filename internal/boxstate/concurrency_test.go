// SPDX-License-Identifier: GPL-2.0-only
package boxstate

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"goteddycloud/internal/events"
	"goteddycloud/internal/protocol"
)

func TestMetadataCallbackDoesNotBlockOtherBoxes(t *testing.T) {
	store := New(events.New())
	started, release := make(chan struct{}), make(chan struct{})
	store.ContentInfo = func(string, uint64, uint32) (string, string) { close(started); <-release; return "title", "" }
	data, err := proto.Marshal(&protocol.TonieRtnlRPC{Log2: &protocol.TonieRtnlLog2{Uptime: proto.Uint64(0), Sequence: proto.Uint32(0), Field3: proto.Uint32(0), FunctionGroup: proto.Uint32(RTNL2_FUGR_AUDIO_B), Function: proto.Uint32(RTNL2_FUNC_AUDIO_ID), Field6: []byte{1, 0, 0, 0}}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- store.Ingest("box-one", data) }()
	<-started
	read := make(chan struct{})
	go func() { store.Snapshot("box-two"); close(read) }()
	select {
	case <-read:
	case <-time.After(time.Second):
		t.Error("ContentInfo callback holds global mutex and blocks unrelated box reads")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	<-read
}

func TestMetadataLookupPreservesSameBoxEventOrder(t *testing.T) {
	hub := events.New()
	subscription, unsubscribe, ok := hub.Subscribe()
	if !ok {
		t.Fatal("subscription refused")
	}
	defer unsubscribe()
	store := New(hub)
	started, release := make(chan struct{}), make(chan struct{})
	store.ContentInfo = func(id string, _ uint64, _ uint32) (string, string) {
		// Callback code may read state, including the current box, without reentering
		// a global lock. The in-progress update is committed before its events.
		store.Snapshot(id)
		close(started)
		<-release
		return "title", ""
	}
	audio, err := proto.Marshal(&protocol.TonieRtnlRPC{Log2: &protocol.TonieRtnlLog2{Uptime: proto.Uint64(0), Sequence: proto.Uint32(0), Field3: proto.Uint32(0), FunctionGroup: proto.Uint32(RTNL2_FUGR_AUDIO_B), Function: proto.Uint32(RTNL2_FUNC_AUDIO_ID), Field6: []byte{1, 0, 0, 0}}})
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := proto.Marshal(&protocol.TonieRtnlRPC{Log3: &protocol.TonieRtnlLog3{Datetime: proto.Uint32(1), Field2: proto.Uint32(RTNL3_TYPE_PLAYBACK_STOPPED)}})
	if err != nil {
		t.Fatal(err)
	}
	first := make(chan error, 1)
	go func() { first <- store.Ingest("one", audio) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("callback cannot read state")
	}
	select {
	case event := <-subscription:
		t.Error("event published before its state was committed", event.Name)
	default:
	}
	second, other := make(chan error, 1), make(chan error, 1)
	go func() { second <- store.Ingest("one", stopped) }()
	go func() { other <- store.Ingest("two", stopped) }()
	select {
	case err := <-other:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Error("metadata callback blocked unrelated box ingestion")
	}
	select {
	case err := <-second:
		t.Error("same-box ingestion passed the pending metadata event")
		second <- err
	default:
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if store.Snapshot("one").Playback != "stopped" || store.Snapshot("one").AudioID != 0 {
		t.Fatal("stale audio state overwrote stop")
	}
	var names []string
	for len(subscription) > 0 {
		event := <-subscription
		if event.Box == "one" {
			names = append(names, event.Name)
		}
	}
	expected := []string{"rtnl-raw-log2", "ContentAudioId", "ContentTitle", "rtnl-raw-log3", "playback"}
	if len(names) != len(expected) {
		t.Fatal(names)
	}
	for index := range names {
		if names[index] != expected[index] {
			t.Fatal("same-box events reordered", names)
		}
	}
}
