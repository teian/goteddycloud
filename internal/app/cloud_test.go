// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"goteddycloud/internal/config"
	"goteddycloud/internal/content"
	"goteddycloud/internal/protocol"
)

type mockRequest struct {
	Path   string
	Header http.Header
	Body   []byte
}
type mockUpstream struct {
	requests  []mockRequest
	freshness []byte
}

func (m *mockUpstream) Do(_ context.Context, _ config.Snapshot, _ config.Snapshot, method, path, query string, headers http.Header, body []byte) (*http.Response, error) {
	m.requests = append(m.requests, mockRequest{path, headers.Clone(), append([]byte(nil), body...)})
	data := []byte("mock time")
	kind := "text/plain; charset=utf-8"
	if strings.HasPrefix(path, "/v1/freshness-check") {
		data = m.freshness
		kind = "application/octet-stream; charset=utf-8"
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {kind}}, Body: io.NopCloser(bytes.NewReader(data))}, nil
}
func TestCloudExcludesProtectedTagsAndUsesOverrides(t *testing.T) {
	s := fixtureApp(t)
	snap := s.Settings.Snapshot("")
	mock := &mockUpstream{}
	s.Cloud = mock
	set := func(k, v string) {
		if err := s.Settings.Set("", k, v); err != nil {
			t.Fatal(err)
		}
	}
	set("cloud.enabled", "true")
	set("cloud.enableV1Time", "true")
	if w := perform(s, "GET", "/v1/time", nil, true); w.Body.String() != "mock time" {
		t.Fatal(w.Body.String())
	}
	if len(mock.requests) != 1 {
		t.Fatal(mock.requests)
	}
	const protected = "0000000100000001"
	perform(s, "GET", "/v1/claim/"+protected, nil, true)
	if len(mock.requests) != 1 {
		t.Fatal("nocloud claim leaked")
	}
	// Empty-tag metadata defaults permit forwarding until an authenticated custom
	// tag is detected; absent auth must not be confused with an all-zero password.
	perform(s, "GET", "/v1/claim/0102030405060708", nil, true)
	if len(mock.requests) != 2 {
		t.Fatal("permitted claim not forwarded")
	}
	auth := strings.Repeat("01", 32)
	if err := s.Content.Update(snap, protected, func(m content.Metadata) error {
		m.Set("cloud_override", true)
		m.Set("cloud_ruid", "0807060504030201")
		m.Set("cloud_auth", auth)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	perform(s, "GET", "/v1/claim/"+protected, nil, true)
	last := mock.requests[len(mock.requests)-1]
	if last.Path != "/v1/claim/0807060504030201" || last.Header.Get("Authorization") != "BD "+auth {
		t.Fatal("override mismatch")
	}
	set("toniebox.overrideCloud", "false")
	reply := &protocol.TonieFreshnessCheckResponse{TonieMarked: []uint64{0x0100000001000000, 2}, Field2: proto.Int32(0), Field6: proto.Int32(0), MaxVolSpk: proto.Int32(2), MaxVolHdp: proto.Int32(1), SlapEn: proto.Int32(0), SlapDir: proto.Int32(1), Led: proto.Int32(2)}
	mock.freshness, _ = proto.Marshal(reply)
	request := &protocol.TonieFreshnessCheckRequest{TonieInfos: []*protocol.TonieFCInfo{{Uid: proto.Uint64(0x0100000001000000), AudioId: proto.Uint32(1)}, {Uid: proto.Uint64(2), AudioId: proto.Uint32(1)}}}
	data, _ := proto.Marshal(request)
	w := perform(s, "POST", "/v1/freshness-check", data, true)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	forwarded := new(protocol.TonieFreshnessCheckRequest)
	if err := proto.Unmarshal(mock.requests[len(mock.requests)-1].Body, forwarded); err != nil {
		t.Fatal(err)
	}
	if len(forwarded.TonieInfos) != 1 || forwarded.TonieInfos[0].GetUid() != 2 {
		t.Fatal("protected freshness tag leaked")
	}
	decoded := new(protocol.TonieFreshnessCheckResponse)
	if err := proto.Unmarshal(w.Body.Bytes(), decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.GetMaxVolSpk() != 2 || len(decoded.TonieMarked) != 1 || decoded.TonieMarked[0] != 2 {
		t.Fatal(decoded)
	}
}

func TestCloudTokenHeadersMatchRequestKind(t *testing.T) {
	for _, authorization := range []string{"Bearer web-secret", "", "BD " + strings.Repeat("ab", 32)} {
		request := httptest.NewRequest("GET", "/v1/content/0200000002000000", nil)
		request.Header.Set("Authorization", authorization)
		if got := cloudHeaders(request.Header).Get("Authorization"); got != "" {
			t.Fatal("non-tag token forwarded", got)
		}
		_, headers := cloudTagRequest(request, content.Metadata{}, "0200000002000000")
		expected := "BD " + strings.Repeat("0", 64)
		if strings.HasPrefix(authorization, "BD ") {
			expected = strings.ToUpper(authorization)
		}
		if got := headers.Get("Authorization"); got != expected {
			t.Fatal(got)
		}
		if request.Header.Get("Authorization") != authorization {
			t.Fatal("incoming header mutated")
		}
	}
}

func TestContentForwardingV2FlagAndV3ProtectedMetadata(t *testing.T) {
	s := fixtureApp(t)
	s.Settings.Set("", "cloud.enabled", "true")
	s.Settings.Set("", "cloud.enableV2Content", "false")
	mock := &mockUpstream{}
	s.Cloud = mock
	if reply := perform(s, "GET", "/v1/content/0200000002000000", nil, true); reply.Code != 404 || len(mock.requests) != 0 {
		t.Fatal("disabled V1 forwarded", reply.Code, len(mock.requests))
	}
	perform(s, "GET", "/v2/content/0300000003000000", nil, true)
	if len(mock.requests) != 0 {
		t.Fatal("disabled V2 forwarded")
	}
	if err := s.Content.Update(s.Settings.Snapshot(""), "0400000004000000", func(m content.Metadata) error { m.Set("nocloud", true); return nil }); err != nil {
		t.Fatal(err)
	}
	reply := perform(s, "GET", "/v3/content-meta/0400000004000000", nil, true)
	if reply.Code != 404 || len(mock.requests) != 0 {
		t.Fatal("protected V3 metadata forwarded", reply.Code, len(mock.requests))
	}
}
