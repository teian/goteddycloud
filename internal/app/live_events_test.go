// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"goteddycloud/internal/boxstate"
	"goteddycloud/internal/content"
	"goteddycloud/internal/protocol"
)

func TestRTNLTagDiscoveryStreamsImmediatelyWithoutAudio(t *testing.T) {
	for _, function := range []uint32{
		boxstate.RTNL2_FUNC_TAG_VALID_CC3200, boxstate.RTNL2_FUNC_TAG_VALID_ESP32,
		boxstate.RTNL2_FUNC_TAG_INVALID_CC3200, boxstate.RTNL2_FUNC_TAG_INVALID_ESP32,
	} {
		t.Run(strconv.FormatUint(uint64(function), 10), func(t *testing.T) {
			s := fixtureApp(t)
			s.Now = func() time.Time { return time.Unix(1800000000, 0) }
			const id, ruid = "001122334455", "11000000500304e0"
			server := httptest.NewServer(http.HandlerFunc(s.Management))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			request, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/sse", nil)
			reply, err := server.Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer reply.Body.Close()
			if reply.StatusCode != 200 || reply.Header.Get("X-Accel-Buffering") != "no" ||
				!strings.Contains(reply.Header.Get("Cache-Control"), "no-cache") {
				t.Fatal(reply.StatusCode, reply.Header)
			}
			uid, _ := content.UIDFromRUID(ruid)
			payload := make([]byte, 8)
			binary.BigEndian.PutUint32(payload, uint32(uid))
			binary.BigEndian.PutUint32(payload[4:], uint32(uid>>32))
			wire, err := proto.Marshal(&protocol.TonieRtnlRPC{Log2: &protocol.TonieRtnlLog2{
				Uptime: proto.Uint64(1), Sequence: proto.Uint32(1), Field3: proto.Uint32(0),
				FunctionGroup: proto.Uint32(boxstate.RTNL2_FUGR_TAG), Function: proto.Uint32(function), Field6: payload,
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.Boxes.Ingest(id, wire); err != nil {
				t.Fatal(err)
			}
			s.updateRTNLSettings(id, wire)
			reader := bufio.NewScanner(reply.Body)
			for reader.Scan() {
				if reader.Text() != "event: tag-present" {
					continue
				}
				if !reader.Scan() {
					t.Fatal("missing SSE payload")
				}
				var event struct {
					Type string
					Data struct {
						Box, RUID string
						Time      int64
					}
				}
				if err := json.Unmarshal([]byte(strings.TrimPrefix(reader.Text(), "data: ")), &event); err != nil {
					t.Fatal(err)
				}
				if event.Type != "tag-present" || event.Data.Box != id || event.Data.RUID != ruid || event.Data.Time != 1800000000 {
					t.Fatal(event)
				}
				// The event must only become visible after the tag API can read it.
				info, err := s.tagInfo(ctx, s.Settings.Snapshot(id), id, ruid)
				if err != nil || info["exists"] != false || info["hide"] != false {
					t.Fatal(info, err)
				}
				if got := s.Settings.Snapshot(id).String("internal.last_ruid"); got != ruid {
					t.Fatal("last tag not updated", got)
				}
				if got := s.Settings.Snapshot("AABBCCDDEEFF").String("internal.last_ruid"); got == ruid {
					t.Fatal("tag leaked into another box")
				}
				return
			}
			t.Fatal("no discovery event", reader.Err())
		})
	}
}

func TestTagEditsPublishOnlyAfterMetadataCommit(t *testing.T) {
	s := fixtureApp(t)
	stream, unsubscribe, ok := s.Events.Subscribe()
	if !ok {
		t.Fatal("subscribe")
	}
	defer unsubscribe()
	const ruid = "11000000500304e0"
	reply := perform(s, "POST", "/content/json/set/"+ruid+"?overlay=001122334455", []byte("tonie_model=NEW_MODEL"), false)
	if reply.Code != 200 {
		t.Fatal(reply.Code, reply.Body.String())
	}
	select {
	case event := <-stream:
		if event.Name != "tag-updated" || event.Box != "001122334455" {
			t.Fatal(event)
		}
		metadata, err := s.Content.Metadata(s.Settings.Snapshot(event.Box), ruid)
		if err != nil || metadata.String("tonie_model") != "NEW_MODEL" {
			t.Fatal("event preceded commit", metadata, err)
		}
	default:
		t.Fatal("no live edit event")
	}
}

func TestTagSnapshotExcludesUnownedBoxesAndInvalidTags(t *testing.T) {
	s := fixtureApp(t)
	user, err := saltedUser("member", "secret")
	if err != nil {
		t.Fatal(err)
	}
	user.Boxes = []string{"001122334455"}
	if err := s.saveWebUsers([]webUser{user}); err != nil {
		t.Fatal(err)
	}
	child, err := s.userArea(user)
	if err != nil {
		t.Fatal(err)
	}
	const ruid = "11000000500304e0"
	child.tagPresented(context.Background(), child.Settings.Snapshot("001122334455"), "001122334455", ruid)
	// Even a stale overlay must not expose a box after ownership changes.
	if err := child.Settings.SetRuntime("AABBCCDDEEFF", map[string]string{
		"internal.last_ruid": "22000000500304e0", "internal.last_ruid_time": "1",
	}); err != nil {
		t.Fatal(err)
	}
	state := child.tagState()
	if len(state) != 1 || state[0]["box"] != "001122334455" || state[0]["ruid"] != ruid {
		t.Fatal("incorrect scoped snapshot", state)
	}
	for _, invalid := range []string{"", "invalid", "0000000000000000", "ffffffffffffffff"} {
		if err := child.Settings.SetRuntime("001122334455", map[string]string{"internal.last_ruid": invalid}); err != nil {
			t.Fatal(err)
		}
		if state := child.tagState(); len(state) != 0 {
			t.Fatal("invalid tag in reconnect snapshot", state)
		}
	}
}
