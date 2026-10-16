// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goteddycloud/internal/content"
)

func TestCloudDiagnosticsDoNotDiscloseCredentials(t *testing.T) {
	s := fixtureApp(t)
	if err := s.Settings.Set("", "cloud.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	defer slog.SetDefault(previous)
	s.Cloud = statusCloud(func(context.Context, http.Header) (*http.Response, error) {
		return nil, errors.New("credential-in-error-must-remain-private")
	})
	_, _ = s.doCloud(context.Background(), s.Settings.Snapshot(""), s.Settings.Snapshot(""), "GET", "/v1/time", "secret-query", http.Header{"Authorization": {"secret-auth"}}, nil)
	log := output.String()
	if !strings.Contains(log, "reason=request_failed") || !strings.Contains(log, "route=/v1/time") {
		t.Fatal(log)
	}
	for _, secret := range []string{"credential-in-error", "secret-query", "secret-auth"} {
		if strings.Contains(log, secret) {
			t.Fatal("credential disclosed", log)
		}
	}
	if got := cloudLogTagPrefix("/v2/content/97763c1a500304e0"); got != "97763c1a" {
		t.Fatal(got)
	}
	if got := cloudLogRoute("/v2/content/private-tag-id"); got != "/v2/content" {
		t.Fatal(got)
	}
	if got := cloudLogRoute("/v1/private-token/anything"); got != "other" {
		t.Fatal(got)
	}
	if got := cloudLogReason(context.DeadlineExceeded); got != "timeout" {
		t.Fatal(got)
	}
}

func TestTagCredentialDiagnosticsReportSourceAndMismatchWithoutTokens(t *testing.T) {
	s := fixtureApp(t)
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)
	const ruid = "97763c1a500304e0"
	metadata := content.Metadata{}
	metadata.Set("cloud_auth", strings.Repeat("AB", 32))
	metadata.Set("cloud_ruid", "b4114322500304e0")
	r := httptest.NewRequest("GET", "/v2/content/"+ruid+"?secret-query=value", nil)
	r.Header.Set("User-Agent", "secret-user-agent")
	headers := http.Header{"Authorization": {"BD " + strings.Repeat("CD", 32)}, "User-Agent": {"secret-user-agent"}}
	if err := s.Settings.Set("", "cloud.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	s.Cloud = &mockUpstream{}
	res, err := s.doCloud(tagCloudContext(r, ruid, metadata, headers), s.Settings.Snapshot("001122334455"), s.Settings.Snapshot(""), "GET", r.URL.Path, r.URL.RawQuery, headers, nil)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	log := output.String()
	for _, expected := range []string{"cloud_request=1", "auth_source=saved", "outgoing_auth_matches_saved=false", "saved_ruid=different", "tag_prefix=97763c1a"} {
		if !strings.Contains(log, expected) {
			t.Fatal("missing credential diagnostic", expected, log)
		}
	}
	for _, secret := range []string{strings.Repeat("AB", 32), strings.Repeat("CD", 32), ruid, "b4114322500304e0", "secret-query", "secret-user-agent"} {
		if strings.Contains(log, secret) {
			t.Fatal("credential disclosed", secret)
		}
	}
}

func TestInterruptedCloudBodyIsDiagnosedOnce(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	defer slog.SetDefault(previous)
	s := fixtureApp(t)
	if err := s.Settings.Set("", "cloud.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	s.Cloud = statusCloud(func(context.Context, http.Header) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(failingCloudReader{})}, nil
	})
	response, err := s.doCloud(context.Background(), s.Settings.Snapshot(""), s.Settings.Snapshot(""), "GET", "/v1/time", "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data := make([]byte, 1)
	_, _ = response.Body.Read(data)
	_, _ = response.Body.Read(data)
	if strings.Count(output.String(), "Tonies cloud audio transfer interrupted") != 1 {
		t.Fatal(output.String())
	}
}

type failingCloudReader struct{}

func (failingCloudReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
