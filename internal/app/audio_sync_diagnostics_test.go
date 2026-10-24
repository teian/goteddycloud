// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"goteddycloud/internal/content"
	"goteddycloud/internal/events"
	"goteddycloud/internal/protocol"
	"goteddycloud/internal/taf"
)

func nextAudioSync(t *testing.T, events <-chan events.Event) map[string]json.RawMessage {
	t.Helper()
	for {
		select {
		case event := <-events:
			if event.Name == "audio-sync" {
				var data map[string]json.RawMessage
				if err := json.Unmarshal(event.Data, &data); err != nil {
					t.Fatal(err)
				}
				return data
			}
		case <-time.After(time.Second):
			t.Fatal("missing synchronization diagnostic")
		}
	}
}

func TestAudioSyncDiagnosticsReportedTagAndSource(t *testing.T) {
	for _, state := range []string{"ready", "missing", "taf_payload_mismatch"} {
		t.Run(state, func(t *testing.T) {
			s := fixtureApp(t)
			const box, ruid, source = "6CC374F2CBD8", "be3cea0e500304e0", "lib://by/audioID/1748955785.taf"
			s.MQTT.Version = "diagnostic-test"
			snap := s.Settings.Snapshot(box)
			if err := s.Content.Update(snap, ruid, func(m content.Metadata) error {
				m.Set("source", source)
				m.Set("nocloud", true)
				m.Set("cloud_auth", "never-export-this-token")
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if state != "missing" {
				data := syntheticTAF(t)
				header, _ := taf.ReadHeader(bytes.NewReader(data))
				header.AudioId = proto.Uint32(1748955785)
				encoded, err := taf.MarshalHeader(header)
				if err != nil {
					t.Fatal(err)
				}
				copy(data, encoded)
				if state != "ready" {
					data[len(data)-1] ^= 1
				}
				path := filepath.Join(snap.String("internal.librarydirfull"), "by/audioID/1748955785.taf")
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			stream, stop, _ := s.Events.Subscribe()
			defer stop()
			// Fixed wire bytes avoid deriving both request and expectation with
			// the application's UID conversion. These are the user's SD audio ID.
			wire, _ := hex.DecodeString("0a0e09be3cea0e500304e015db4f6961")
			r := httptest.NewRequest("POST", "/v1/freshness-check?secret-query=hidden", bytes.NewReader(wire))
			r.Header.Set("Authorization", "secret-header")
			w := httptest.NewRecorder()
			s.freshnessCheck(w, r, box, snap)
			if w.Code != 200 {
				t.Fatal(w.Code)
			}
			diagnostic := nextAudioSync(t, stream)
			var tags []audioSyncDecision
			if err := json.Unmarshal(diagnostic["tags"], &tags); err != nil || len(tags) != 1 {
				t.Fatal(err, string(diagnostic["tags"]))
			}
			tag := tags[0]
			if tag.RUID != ruid || tag.Source != source || tag.BoxAudioID != 1634291675 || !tag.NoCloud || tag.LocalState != state {
				t.Fatal(tag)
			}
			if state == "ready" {
				if tag.LocalAudioID == nil || *tag.LocalAudioID != 1748955785 || !tag.Marked || !tag.Pending {
					t.Fatal(tag)
				}
			} else if tag.LocalAudioID != nil || tag.Marked {
				t.Fatal("unusable source reported as ready", tag)
			}
			encoded, _ := json.Marshal(diagnostic)
			for _, secret := range []string{"never-export-this-token", "secret-query", "secret-header"} {
				if bytes.Contains(encoded, []byte(secret)) {
					t.Fatal("diagnostic disclosed credentials")
				}
			}
		})
	}
}

func TestAudioSyncDiagnosticsEmptyRejectedAndV3Requests(t *testing.T) {
	s := fixtureApp(t)
	stream, stop, _ := s.Events.Subscribe()
	defer stop()
	for _, test := range []struct {
		v3     bool
		body   []byte
		status int
	}{{false, nil, 200}, {false, []byte{0x0a, 2, 0x09, 0}, 400}, {false, bytes.Repeat([]byte{0}, 4096), 413},
		{true, []byte(`{"content":{"be3cea0e500304e0":1634291675}}`), 200}, {true, []byte("{"), 400}, {true, bytes.Repeat([]byte{0}, 4096), 413}} {
		route := "/v1/freshness-check"
		if test.v3 {
			route = "/v3/freshness-check"
		}
		r := httptest.NewRequest("POST", route, bytes.NewReader(test.body))
		w := httptest.NewRecorder()
		if test.v3 {
			s.v3Freshness(w, r, "001122334455", s.Settings.Snapshot("001122334455"))
		} else {
			s.freshnessCheck(w, r, "001122334455", s.Settings.Snapshot("001122334455"))
		}
		data := nextAudioSync(t, stream)
		if w.Code != test.status || string(data["status"]) != fmt.Sprint(test.status) || string(data["route"]) != fmt.Sprintf("%q", route) {
			t.Fatal(w.Code, data)
		}
	}
}

func TestAudioSyncDiagnosticsBoundedFramesAndFinalCloudMarks(t *testing.T) {
	s := fixtureApp(t)
	stream, stop, _ := s.Events.Subscribe()
	defer stop()
	_, trace := newAudioSyncTrace(context.Background())
	reply := &protocol.TonieFreshnessCheckResponse{}
	for i := 0; i < 240; i++ {
		ruid := content.RUID(uint64(i + 1))
		trace.decisions = append(trace.decisions, audioSyncDecision{RUID: ruid, Source: "lib://" + strings.Repeat("\x00", 500)})
		reply.TonieMarked = append(reply.TonieMarked, uint64(i+1))
	}
	s.traceFreshness("box", "/v1/freshness-check", trace, 200, reply)
	for i := 0; i < 15; i++ {
		data := nextAudioSync(t, stream)
		encoded, _ := json.Marshal(map[string]any{"type": "audio-sync", "box": "box", "data": data})
		if len(encoded) > 65536 {
			t.Fatal("oversized stream frame", len(encoded))
		}
		var tags []audioSyncDecision
		if err := json.Unmarshal(data["tags"], &tags); err != nil || len(tags) != 16 || !tags[0].Marked || len(tags[0].Reasons) != 1 || tags[0].Reasons[0] != "cloud" {
			t.Fatal("cloud response not reflected in diagnostic", err, tags)
		}
	}
	if audioSyncSource("https://user:password@example.test/private?token=secret") != "remote-stream" || audioSyncLocalState(errors.New("secret-path")) != "storage_failure" {
		t.Fatal("unsafe diagnostic")
	}
}
