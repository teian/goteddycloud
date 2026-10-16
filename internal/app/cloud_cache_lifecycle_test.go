// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goteddycloud/internal/config"
	"goteddycloud/internal/content"
)

type claimRejectedContentCloud struct {
	data  []byte
	paths []string
}

func TestCloudCacheFailuresAreVisibleAndNeverPublishInvalidAudio(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "corrupt payload", true: "caching disabled"}[disabled], func(t *testing.T) {
			s := fixtureApp(t)
			for _, key := range []string{"cloud.enabled", "cloud.cacheContent", "cloud.cacheToLibrary"} {
				if err := s.Settings.Set("", key, "true"); err != nil {
					t.Fatal(err)
				}
			}
			data := syntheticTAF(t)
			if disabled {
				if err := s.Settings.Set("", "cloud.cacheContent", "false"); err != nil {
					t.Fatal(err)
				}
			} else {
				data[len(data)-1] ^= 1
			}
			s.Cloud = &contentCloud{data: data, length: int64(len(data))}
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			defer slog.SetDefault(previous)
			reply := perform(s, "GET", "/content/download/97763C1A/500304E0", nil, false)
			if reply.Code != 200 || !bytes.Equal(reply.Body.Bytes(), data) {
				t.Fatal("transfer changed", reply.Code)
			}
			snap := s.Settings.Snapshot("")
			if file, _, err := s.Content.Open(snap, snap, "97763c1a500304e0"); err == nil {
				file.Close()
				t.Fatal("invalid or disabled cache published")
			}
			expected := "stage=publish reason=taf_payload_mismatch"
			if disabled {
				expected = "reason=caching_disabled"
			}
			if !strings.Contains(logs.String(), expected) || strings.Contains(logs.String(), "Tonies audio cache published") {
				t.Fatal("cache result not diagnosed correctly", logs.String())
			}
		})
	}
}

func (c *claimRejectedContentCloud) Do(_ context.Context, _, _ config.Snapshot, _, path, _ string, _ http.Header, _ []byte) (*http.Response, error) {
	c.paths = append(c.paths, path)
	status, data := 200, c.data
	if strings.HasPrefix(path, "/v1/claim/") {
		status, data = 401, nil
	}
	return &http.Response{StatusCode: status, Header: http.Header{}, ContentLength: int64(len(data)), Body: io.NopCloser(bytes.NewReader(data))}, nil
}

type cancelAfterBodyWriter struct {
	*httptest.ResponseRecorder
	remaining int
	cancel    context.CancelFunc
}

func (w *cancelAfterBodyWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseRecorder.Write(data)
	w.remaining -= n
	if w.remaining <= 0 {
		w.cancel()
	}
	return n, err
}

func TestCloudCacheAfterRejectedClaimSurvivesCompletedClientDisconnect(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		t.Run(map[bool]string{false: "connected", true: "disconnect after complete body"}[disconnect], func(t *testing.T) {
			s := fixtureApp(t)
			for _, key := range []string{"cloud.enabled", "cloud.enableV1Claim", "cloud.enableV2Content", "cloud.cacheContent", "cloud.cacheToLibrary"} {
				if err := s.Settings.Set("", key, "true"); err != nil {
					t.Fatal(err)
				}
			}
			const ruid = "97763c1a500304e0"
			data := syntheticTAF(t)
			mock := &claimRejectedContentCloud{data: data}
			s.Cloud = mock
			claim := httptest.NewRequest("GET", "/v1/claim/"+ruid, nil)
			claim.Header.Set("Authorization", "BD "+strings.Repeat("AB", 32))
			claimed := httptest.NewRecorder()
			s.Device(claimed, claim)
			if claimed.Code != 401 {
				t.Fatal("claim must reproduce upstream rejection", claimed.Code)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			request := httptest.NewRequest("GET", "/content/download/97763C1A/500304E0", nil).WithContext(ctx)
			reply := httptest.NewRecorder()
			if disconnect {
				s.Management(&cancelAfterBodyWriter{ResponseRecorder: reply, remaining: len(data), cancel: cancel}, request)
			} else {
				s.Management(reply, request)
			}
			if reply.Code != 200 || !bytes.Equal(reply.Body.Bytes(), data) {
				t.Fatal("audio download failed", reply.Code)
			}
			snap := s.Settings.Snapshot("")
			metadata, err := s.Content.Metadata(snap, ruid)
			if err != nil || metadata.String("source") != "lib://by/audioID/123456.taf" {
				t.Fatal("completed download was not persisted to library", metadata.String("source"), err)
			}
			stored, err := os.ReadFile(filepath.Join(snap.String("internal.librarydirfull"), "by/audioID/123456.taf"))
			if err != nil || !bytes.Equal(stored, data) {
				t.Fatal("persisted audio differs", err)
			}
			// A new content store must serve the file with the cloud disabled.
			reopened := content.New()
			defer reopened.Close()
			file, _, err := reopened.Open(snap, snap, ruid)
			if err != nil {
				t.Fatal("persisted audio cannot be reopened", err)
			}
			file.Close()
			if err := s.Settings.Set("", "cloud.enabled", "false"); err != nil {
				t.Fatal(err)
			}
			cached := perform(s, "GET", request.URL.String(), nil, false)
			if cached.Code != 200 || !bytes.Equal(cached.Body.Bytes(), data) || len(mock.paths) != 2 {
				t.Fatal("cached download called upstream or lost data", cached.Code, mock.paths)
			}
		})
	}
}
