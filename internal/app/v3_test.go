// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"goteddycloud/internal/config"
	"goteddycloud/internal/taf"
)

type v3Cloud struct {
	path          string
	body          []byte
	authorization string
}

func (c *v3Cloud) Do(_ context.Context, _ config.Snapshot, _ config.Snapshot, _, path, _ string, headers http.Header, data []byte) (*http.Response, error) {
	c.path = path
	c.body = append([]byte(nil), data...)
	c.authorization = headers.Get("Authorization")
	reply := []byte(`{"items":["0000000100000001","0200000002000000"]}`)
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json; charset=utf-8"}}, Body: io.NopCloser(bytes.NewReader(reply))}, nil
}
func TestV3FreshnessExcludesProtectedTagsAndFiltersMarks(t *testing.T) {
	s := fixtureApp(t)
	if err := s.Settings.Set("", "cloud.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	mock := new(v3Cloud)
	s.Cloud = mock
	reply := perform(s, "POST", "/v3/freshness-check", []byte(`{"content":{"0000000100000001":0,"0200000002000000":10}}`), true)
	if reply.Code != 200 {
		t.Fatal(reply.Code)
	}
	var requested struct {
		Content map[string]uint32 `json:"content"`
	}
	if err := json.Unmarshal(mock.body, &requested); err != nil {
		t.Fatal(err)
	}
	if len(requested.Content) != 1 || requested.Content["0200000002000000"] != 10 {
		t.Fatal("protected tag forwarded", requested)
	}
	if mock.authorization != "" || mock.path != "/v3/freshness-check" {
		t.Fatal(mock)
	}
	var result struct {
		Items []string `json:"items"`
	}
	if err := json.Unmarshal(reply.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 1 || result.Items[0] != "0200000002000000" {
		t.Fatal(result)
	}
}
func TestV3ChapterBytesAndRangeAcrossRepeatedHeader(t *testing.T) {
	s := fixtureApp(t)
	snap := s.Settings.Snapshot("")
	path := filepath.Join(snap.String("internal.librarydirfull"), "synthetic.taf")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := taf.NewWriter(file, 123456)
	if err != nil {
		t.Fatal(err)
	}
	head := []byte{'O', 'p', 'u', 's', 'H', 'e', 'a', 'd', 1, 2, 0x38, 1, 0x80, 0xbb, 0, 0, 0, 0, 0}
	for range 2 {
		if err := writer.StartTrack(head); err != nil {
			t.Fatal(err)
		}
		if err := writer.Packet([]byte{0xfb, 3, 0xff, 0xfe, 0xff, 0xfe, 0xff, 0xfe}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	file.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reply := perform(s, "GET", "/v3/chapter/teddycloud_01_0000000100000001", nil, true)
	expected := append(append([]byte(nil), data[4096:4608]...), data[8192:]...)
	if reply.Code != 200 || !bytes.Equal(reply.Body.Bytes(), expected) {
		t.Fatal("chapter bytes", reply.Code, reply.Body.Len())
	}
	request := httptest.NewRequest("GET", "/v3/chapter/teddycloud_01_0000000100000001", nil)
	request.Header.Set("Range", "bytes=508-516")
	partial := httptest.NewRecorder()
	s.Device(partial, request)
	if partial.Code != 206 || !bytes.Equal(partial.Body.Bytes(), expected[508:517]) {
		t.Fatal("chapter prefix range", partial.Code)
	}
	metadata := perform(s, "GET", "/v3/content-meta/0000000100000001", nil, true)
	var result struct {
		Version uint32 `json:"version"`
		Content []struct {
			FileSize int `json:"fileSize"`
		} `json:"content"`
	}
	if err := json.Unmarshal(metadata.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Version != 123456 || len(result.Content) != 2 || result.Content[0].FileSize != 4096 || result.Content[1].FileSize != 4608 {
		t.Fatal(result)
	}
}
