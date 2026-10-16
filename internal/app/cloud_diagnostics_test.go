// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
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
