// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"google.golang.org/protobuf/proto"
	"goteddycloud/internal/content"
	"goteddycloud/internal/protocol"
)

const recognitionCatalog = `[{"model":"AUTO_TEST","series":"Test series","episodes":"Test episode","audio_id":["1610612753"],"hash":["00112233445566778899aabbccddeeff00112233"],"pic":"https://images.invalid/test.png"},{"model":"SPECIAL","audio_id":[1],"hash":["00112233445566778899aabbccddeeff00112233"]}]`

func TestFreshnessRecognizesTagsWithoutLocalAudio(t *testing.T) {
	for _, kind := range []string{"v1", "v3"} {
		for _, audioID := range []uint32{0x60000011, 0x10000011} {
			t.Run(fmt.Sprintf("%s/%x", kind, audioID), func(t *testing.T) {
				s := fixtureApp(t)
				if err := s.Catalog.ReplaceOfficial([]byte(recognitionCatalog)); err != nil {
					t.Fatal(err)
				}
				const ruid = "11000000500304e0"
				uid, _ := content.UIDFromRUID(ruid)
				request := &protocol.TonieFreshnessCheckRequest{TonieInfos: []*protocol.TonieFCInfo{{Uid: proto.Uint64(uid), AudioId: proto.Uint32(audioID)}}}
				payload, err := proto.Marshal(request)
				if err != nil {
					t.Fatal(err)
				}
				path := "/v1/freshness-check"
				if kind == "v3" {
					path = "/v3/freshness-check"
					payload, _ = json.Marshal(map[string]any{"content": map[string]uint32{ruid: audioID}})
				}
				if reply := perform(s, "POST", path, payload, true); reply.Code != 200 {
					t.Fatal(reply.Code, reply.Body.String())
				}
				reply := perform(s, "GET", "/api/getTagInfo?ruid="+ruid, nil, false)
				var result struct {
					Tag struct {
						Exists bool           `json:"exists"`
						Info   map[string]any `json:"tonieInfo"`
					} `json:"tagInfo"`
				}
				if reply.Code != 200 || json.Unmarshal(reply.Body.Bytes(), &result) != nil {
					t.Fatal(reply.Code, reply.Body.String())
				}
				if result.Tag.Exists || result.Tag.Info["model"] != "AUTO_TEST" || result.Tag.Info["series"] != "Test series" || result.Tag.Info["episode"] != "Test episode" || result.Tag.Info["picture"] != "https://images.invalid/test.png" {
					t.Fatal("tag not recognized without a download", reply.Body.String())
				}
				m, err := s.Content.Metadata(s.Settings.Snapshot(""), ruid)
				if err != nil || m.String("tonie_model") != "AUTO_TEST" || boxAudioID(m) != audioID {
					t.Fatal("recognition was not persisted", err)
				}
			})
		}
	}
}

func TestFreshnessRecognitionRetainsAudioIDForLaterCatalog(t *testing.T) {
	s := fixtureApp(t)
	const id, ruid = "001122334455", "12000000500304e0"
	if err := s.Settings.Set(id, "core.contentdir", "recognition-test"); err != nil {
		t.Fatal(err)
	}
	snap := s.Settings.Snapshot(id)
	if err := os.MkdirAll(snap.String("internal.contentdirfull"), 0700); err != nil {
		t.Fatal(err)
	}
	uid, _ := content.UIDFromRUID(ruid)
	request := &protocol.TonieFreshnessCheckRequest{TonieInfos: []*protocol.TonieFCInfo{{Uid: proto.Uint64(uid), AudioId: proto.Uint32(0x60000011)}}}
	s.localFreshness(id, snap, request)
	// The catalog was unavailable when the box reported the tag. A later UI
	// refresh must resolve it from persisted metadata, even after a restart.
	if err := s.Catalog.ReplaceOfficial([]byte(recognitionCatalog)); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(s.Settings)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	info, err := reopened.tagInfo(context.Background(), snap, id, ruid)
	if err != nil || info["tonieInfo"].(map[string]any)["model"] != "AUTO_TEST" {
		t.Fatal(info, err)
	}
	m, err := reopened.Content.Metadata(snap, ruid)
	if err != nil || m.String("tonie_model") != "AUTO_TEST" {
		t.Fatal("late recognition was not persisted", err)
	}
	global, err := reopened.Content.Metadata(s.Settings.Snapshot(""), ruid)
	if err != nil || global.String("tonie_model") != "" || boxAudioID(global) != 0 {
		t.Fatal("recognition leaked into a different content overlay", err)
	}
}

func TestFreshnessRecognitionPreservesManualAndSpecialModels(t *testing.T) {
	for _, test := range []struct {
		name, model string
		audioID     uint32
	}{
		{"manual", "MANUAL_MODEL", 0x60000011},
		{"unknown", "PREVIOUS_MODEL", 0x6fffffff},
		{"special", "", 1},
		{"zero", "", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := fixtureApp(t)
			if err := s.Catalog.ReplaceOfficial([]byte(recognitionCatalog)); err != nil {
				t.Fatal(err)
			}
			snap := s.Settings.Snapshot("")
			const ruid = "13000000500304e0"
			if err := s.Content.Update(snap, ruid, func(m content.Metadata) error {
				m.Set("tonie_model", test.model)
				m.Set("fixture_extension", "preserved")
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			uid, _ := content.UIDFromRUID(ruid)
			s.localFreshness("", snap, &protocol.TonieFreshnessCheckRequest{TonieInfos: []*protocol.TonieFCInfo{{Uid: proto.Uint64(uid), AudioId: proto.Uint32(test.audioID)}}})
			if _, err := s.tagInfo(context.Background(), snap, "", ruid); err != nil {
				t.Fatal(err)
			}
			m, err := s.Content.Metadata(snap, ruid)
			if err != nil || m.String("tonie_model") != test.model || m.String("fixture_extension") != "preserved" {
				t.Fatal("existing metadata replaced or ambiguous model guessed", err)
			}
		})
	}
}

func TestAssignmentConsumedByOnlyOneUnknownTag(t *testing.T) {
	s := fixtureApp(t)
	snap := s.Settings.Snapshot("")
	source := filepath.Join(snap.String("internal.librarydirfull"), "synthetic.taf")
	if err := os.WriteFile(source, syntheticTAF(t), 0600); err != nil {
		t.Fatal(err)
	}
	// The synthetic fixture is already a valid, bounded TAF.
	if err := s.Settings.SetRuntime("", map[string]string{"internal.assign_unknown": source}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, uid := range []string{"1100000011000000", "1200000012000000"} {
		wg.Add(1)
		go func(uid string) { defer wg.Done(); codes <- perform(s, "GET", "/v1/content/"+uid, nil, true).Code }(uid)
	}
	wg.Wait()
	close(codes)
	success := 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code != 404 {
			t.Fatal(code)
		}
	}
	if success != 1 {
		t.Fatal("assignment reused", success)
	}
	if s.Settings.Snapshot("").String("internal.assign_unknown") != "" {
		t.Fatal("pending assignment retained")
	}
	before, _ := os.ReadFile(source)
	if len(before) == 0 {
		t.Fatal("source damaged")
	}
}
func TestTagInfoListingsAndTriggerHooks(t *testing.T) {
	s := fixtureApp(t)
	if err := os.WriteFile(filepath.Join(s.Settings.Snapshot("").String("internal.librarydirfull"), "synthetic.taf"), syntheticTAF(t), 0600); err != nil {
		t.Fatal(err)
	}
	result := perform(s, "GET", "/api/getTagInfo?ruid=0000000100000001", nil, false)
	var object struct {
		Tag map[string]any `json:"tagInfo"`
	}
	if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &object) != nil {
		t.Fatal(result.Code, result.Body.String())
	}
	if object.Tag["uid"] != "01:00:00:00:01:00:00:00" || object.Tag["type"] != "system" || object.Tag["exists"] != true {
		t.Fatal(object.Tag)
	}
	calls := make(chan bool, 2)
	s.Shutdown = func(restart bool) { calls <- restart }
	for _, tc := range []struct{ path, body string }{{"triggerUnknown", "FAILED"}, {"triggerWriteConfig", "OK"}, {"triggerReloadConfig", "OK"}, {"triggerRestart", "OK"}, {"triggerExit", "OK"}} {
		result = perform(s, "GET", "/api/"+tc.path, nil, false)
		if result.Code != 200 || result.Header().Get("Content-Type") != "text/plain" || result.Body.String() != tc.body {
			t.Fatal(tc.path, result.Code, result.Body.String())
		}
	}
	if !<-calls || <-calls {
		t.Fatal("wrong lifecycle request")
	}
}
