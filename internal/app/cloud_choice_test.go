// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"net/http/httptest"
	"strings"
	"testing"

	"goteddycloud/internal/content"
)

func TestCustomTagsAutomaticallyBlockCloudAfterManualEnable(t *testing.T) {
	for _, test := range []struct{ ruid, password string }{
		{"7c47c80e500304e0", strings.Repeat("00", 32)},
		{"0102030405060708", strings.Repeat("AB", 32)},
	} {
		t.Run(test.ruid, func(t *testing.T) {
			s := fixtureApp(t)
			const id = "001122334455"
			if err := s.Settings.Set("", "cloud.enabled", "true"); err != nil {
				t.Fatal(err)
			}
			mock := &mockUpstream{}
			s.Cloud = mock
			// Ignore the legacy manual flag written by the earlier experimental image.
			if err := s.Content.Update(s.Settings.Snapshot(id), test.ruid, func(m content.Metadata) error { m.Set("nocloud_manual", true); return nil }); err != nil {
				t.Fatal(err)
			}
			reply := perform(s, "POST", "/content/json/set/"+test.ruid+"?overlay="+id, []byte("nocloud=false"), false)
			if reply.Code != 200 {
				t.Fatal(reply.Code)
			}
			req := httptest.NewRequest("GET", "/v2/content/"+test.ruid, nil)
			req.Header.Set("Authorization", "BD "+test.password)
			reply = httptest.NewRecorder()
			s.serveStored(reply, req, id, s.Settings.Snapshot(id), test.ruid)
			if reply.Code != 404 || len(mock.requests) != 0 {
				t.Fatal("custom tag forwarded to cloud", reply.Code)
			}
			metadata, err := s.Content.Metadata(s.Settings.Snapshot(id), test.ruid)
			if err != nil || !metadata.Bool("nocloud") {
				t.Fatal("custom tag not blocked", err)
			}
		})
	}
}

func TestOriginalFigurineRemainsCloudEnabled(t *testing.T) {
	s := fixtureApp(t)
	const id, ruid = "001122334455", "97763c1a500304e0"
	if err := s.Settings.Set("", "cloud.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	mock := &mockUpstream{}
	s.Cloud = mock
	req := httptest.NewRequest("GET", "/v2/content/"+ruid, nil)
	req.Header.Set("Authorization", "BD "+strings.Repeat("AB", 32))
	reply := httptest.NewRecorder()
	s.serveStored(reply, req, id, s.Settings.Snapshot(id), ruid)
	metadata, err := s.Content.Metadata(s.Settings.Snapshot(id), ruid)
	if reply.Code != 200 || len(mock.requests) != 1 || err != nil || metadata.Bool("nocloud") {
		t.Fatal("original figurine blocked", reply.Code, err)
	}
}
