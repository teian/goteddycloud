// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"
	"goteddycloud/internal/content"
	"goteddycloud/internal/protocol"
	"goteddycloud/internal/taf"
)

type failedLocalBodyWriter struct{ *httptest.ResponseRecorder }

func (w failedLocalBodyWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestCustomAudioChangesWithSameOrLowerIDRemainPendingPerBox(t *testing.T) {
	for _, targetID := range []uint32{123456, 123455} {
		t.Run(fmt.Sprint(targetID), func(t *testing.T) {
			s := fixtureApp(t)
			const box, other, ruid = "001122334455", "AABBCCDDEEFF", "1100000011000000"
			snap := s.Settings.Snapshot(box)
			path := filepath.Join(snap.String("internal.librarydirfull"), "custom.taf")
			old := syntheticTAF(t)
			if err := os.WriteFile(path, old, 0600); err != nil {
				t.Fatal(err)
			}
			form := url.Values{"source": {"lib://custom.taf"}, "nocloud": {"true"}}.Encode()
			if reply := perform(s, "POST", "/content/json/set/"+ruid+"?overlay="+box, []byte(form), false); reply.Code != 200 {
				t.Fatal(reply.Code)
			}
			uid, _ := content.UIDFromRUID(ruid)
			pending := func(server *Server, id string, audioID uint32) bool {
				t.Helper()
				request := &protocol.TonieFreshnessCheckRequest{TonieInfos: []*protocol.TonieFCInfo{{Uid: proto.Uint64(uid), AudioId: proto.Uint32(audioID)}}}
				return len(server.localFreshness(id, server.Settings.Snapshot(id), request).TonieMarked) != 0
			}
			header, _ := taf.ReadHeader(bytes.NewReader(old))
			s.recordLocalContentDownload(context.Background(), snap, box, ruid, localContentFingerprint(header), 0, 1)
			if pending(s, box, 123456) {
				t.Fatal("unchanged downloaded content marked again")
			}
			updated := syntheticTAF(t, 2)
			header, _ = taf.ReadHeader(bytes.NewReader(updated))
			header.AudioId = proto.Uint32(targetID)
			encoded, err := taf.MarshalHeader(header)
			if err != nil {
				t.Fatal(err)
			}
			copy(updated, encoded)
			if err := os.WriteFile(path, updated, 0600); err != nil {
				t.Fatal(err)
			}
			if !pending(s, box, 123456) {
				t.Fatal("changed local audio was not marked")
			}
			// A browser can preview/download the audio without updating the SD card.
			reply := perform(s, "GET", "/content/download/11000000/11000000?overlay="+box, nil, false)
			if reply.Code != 200 || !bytes.Equal(reply.Body.Bytes(), updated) || !pending(s, box, 123456) {
				t.Fatal("browser download acknowledged box content", reply.Code)
			}
			s.recordLocalContentDownload(context.Background(), snap, box, ruid, localContentFingerprint(header), 0, 1)
			if pending(s, box, targetID) || !pending(s, other, targetID) {
				t.Fatal("download receipt was not isolated per box")
			}
			reopened, err := New(s.Settings)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if pending(reopened, box, targetID) {
				t.Fatal("receipt did not survive reopening")
			}
		})
	}
}

func TestV3LocalContentReceiptWaitsForEveryChapter(t *testing.T) {
	s := fixtureApp(t)
	snap := s.Settings.Snapshot("001122334455")
	const ruid, box = "1100000011000000", "001122334455"
	if err := s.Content.Update(snap, ruid, func(m content.Metadata) error { m.Set("nocloud", true); return nil }); err != nil {
		t.Fatal(err)
	}
	header, err := taf.ReadHeader(bytes.NewReader(syntheticTAF(t)))
	if err != nil {
		t.Fatal(err)
	}
	for _, chapter := range []int{0, 0} {
		s.recordLocalContentDownload(context.Background(), snap, box, ruid, localContentFingerprint(header), chapter, 2)
		metadata, _ := s.Content.Metadata(snap, ruid)
		if !localContentPending(metadata, box, header) {
			t.Fatal("incomplete chapter download was acknowledged")
		}
	}
	s.recordLocalContentDownload(context.Background(), snap, box, ruid, localContentFingerprint(header), 1, 2)
	metadata, _ := s.Content.Metadata(snap, ruid)
	if localContentPending(metadata, box, header) {
		t.Fatal("all chapters were not acknowledged")
	}
}

func TestStoredDownloadFingerprintRequiresCompleteTAF(t *testing.T) {
	s := fixtureApp(t)
	snap := s.Settings.Snapshot("")
	const ruid = "1100000011000000"
	data := syntheticTAF(t)
	if err := os.WriteFile(filepath.Join(snap.String("internal.librarydirfull"), "receipt.taf"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Content.Update(snap, ruid, func(m content.Metadata) error { m.Set("source", "lib://receipt.taf"); return nil }); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/v1/content/"+ruid, nil)
	request.Header.Set("Range", "bytes=4096-")
	if fingerprint := s.serveStoredLocal(httptest.NewRecorder(), request, snap, ruid); fingerprint != "" {
		t.Fatal("partial body acknowledged")
	}
	request = httptest.NewRequest("GET", "/v1/content/"+ruid+"?skip_header=true", nil)
	if fingerprint := s.serveStoredLocal(httptest.NewRecorder(), request, snap, ruid); fingerprint != "" {
		t.Fatal("header-stripped body acknowledged")
	}
	request = httptest.NewRequest("GET", "/v1/content/"+ruid, nil)
	if fingerprint := s.serveStoredLocal(failedLocalBodyWriter{httptest.NewRecorder()}, request, snap, ruid); fingerprint != "" {
		t.Fatal("interrupted body acknowledged")
	}
	if fingerprint := s.serveStoredLocal(httptest.NewRecorder(), request, snap, ruid); fingerprint == "" {
		t.Fatal("complete body not acknowledged")
	}
}
