// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManualCloudChoiceSurvivesCustomTagAutodetection(t *testing.T) {
	for _, ruid := range []string{"7c47c80e500304e0", "0102030405060708"} {
		t.Run(ruid, func(t *testing.T) {
			s := fixtureApp(t)
			const id = "001122334455"
			if err := s.Settings.Set("", "cloud.enabled", "true"); err != nil {
				t.Fatal(err)
			}
			data := syntheticTAF(t)
			cloud := &contentCloud{data: data, length: int64(len(data))}
			s.Cloud = cloud
			request := func() *httptest.ResponseRecorder {
				req := httptest.NewRequest("GET", "/v2/content/"+ruid, nil)
				req.Header.Set("Authorization", "BD "+strings.Repeat("0", 64))
				reply := httptest.NewRecorder()
				s.serveStored(reply, req, id, s.Settings.Snapshot(id), ruid)
				return reply
			}
			// Existing automatic protection is retained for tags without a choice.
			if reply := request(); reply.Code != 404 || cloud.calls.Load() != 0 {
				t.Fatal(reply.Code, cloud.calls.Load())
			}
			reply := perform(s, "POST", "/content/json/set/"+ruid+"?overlay="+id, []byte("nocloud=false"), false)
			if reply.Code != 200 {
				t.Fatal(reply.Code)
			}
			// A new presentation must honor the explicit choice rather than
			// turning the white cloud red again and returning a local 404.
			if reply := request(); reply.Code != 200 || cloud.calls.Load() != 1 || reply.Body.Len() != len(data) {
				t.Fatal(reply.Code, cloud.calls.Load(), reply.Body.Len())
			}
			metadata, err := s.Content.Metadata(s.Settings.Snapshot(id), ruid)
			if err != nil || metadata.Bool("nocloud") || !metadata.Bool("nocloud_manual") {
				t.Fatal(metadata, err)
			}
			if reply := perform(s, "POST", "/content/json/set/"+ruid+"?overlay="+id, []byte("nocloud=true"), false); reply.Code != 200 {
				t.Fatal(reply.Code)
			}
			metadata, _ = s.Content.Metadata(s.Settings.Snapshot(id), ruid)
			if !metadata.Bool("nocloud") || !metadata.Bool("nocloud_manual") {
				t.Fatal(metadata)
			}
		})
	}
}
