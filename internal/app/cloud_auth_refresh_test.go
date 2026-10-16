// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"net/http/httptest"
	"strings"
	"testing"

	"goteddycloud/internal/content"
)

func TestBrowserDownloadDoesNotRedetectCustomTag(t *testing.T) {
	s := fixtureApp(t)
	const id, ruid = "001122334455", "97763c1a500304e0"
	if err := s.Settings.Set("", "cloud.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if err := s.Content.Update(s.Settings.Snapshot(id), ruid, func(m content.Metadata) error {
		m.Set("nocloud", false)
		m.Set("cloud_ruid", ruid)
		m.Set("cloud_auth", strings.Repeat("00", 32))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	mock := &mockUpstream{}
	s.Cloud = mock
	reply := perform(s, "GET", "/content/download/97763C1A/500304E0?overlay="+id, nil, false)
	metadata, err := s.Content.Metadata(s.Settings.Snapshot(id), ruid)
	if err != nil || metadata.Bool("nocloud") {
		t.Fatal("browser download changed cloud policy", reply.Code, err)
	}
	if len(mock.requests) != 1 {
		t.Fatal("browser request was not forwarded", reply.Code)
	}
}

func TestDeviceCredentialsRefreshAndNeverDowngradeToEmptyPassword(t *testing.T) {
	s := fixtureApp(t)
	const id, ruid = "001122334455", "97763c1a500304e0"
	snap := s.Settings.Snapshot(id)
	m := content.Metadata{}
	m.Set("cloud_ruid", ruid)
	m.Set("cloud_auth", strings.Repeat("00", 32))
	request := httptest.NewRequest("GET", "/v2/content/"+ruid, nil)
	request.Header.Set("Authorization", "BD "+strings.Repeat("AB", 32))
	updateTagAuth(m, request, snap, ruid, true)
	if m.String("cloud_auth") != strings.Repeat("AB", 32) {
		t.Fatal("stale empty password was not refreshed")
	}
	request.Header.Set("Authorization", "BD "+strings.Repeat("CD", 32))
	updateTagAuth(m, request, snap, ruid, true)
	if m.String("cloud_auth") != strings.Repeat("CD", 32) {
		t.Fatal("changed device password was not refreshed")
	}
	request.Header.Set("Authorization", "BD "+strings.Repeat("00", 32))
	updateTagAuth(m, request, snap, ruid, false)
	if m.String("cloud_auth") != strings.Repeat("CD", 32) {
		t.Fatal("empty claim password replaced useful credentials")
	}
	m.Set("cloud_override", true)
	request.Header.Set("Authorization", "BD "+strings.Repeat("EF", 32))
	updateTagAuth(m, request, snap, ruid, true)
	if m.String("cloud_auth") != strings.Repeat("CD", 32) {
		t.Fatal("override credentials were changed")
	}
}

func TestCloudAuthStateDoesNotTreatEmptyPasswordsAsUsable(t *testing.T) {
	for _, test := range []struct{ password, state string }{
		{"", "missing"}, {"bad", "missing"}, {strings.Repeat("00", 32), "empty"}, {strings.Repeat("AB", 32), "present"},
	} {
		m := content.Metadata{}
		m.Set("cloud_auth", test.password)
		if cloudAuthState(m) != test.state || hasCloudAuth(m) != (test.state == "present") {
			t.Fatal(test.state, cloudAuthState(m))
		}
	}
}

func TestRefreshedDeviceCredentialsAllowBrowserDownload(t *testing.T) {
	s := fixtureApp(t)
	const id, ruid = "001122334455", "97763c1a500304e0"
	if err := s.Settings.Set("", "cloud.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	snap := s.Settings.Snapshot(id)
	if err := s.Content.Update(snap, ruid, func(m content.Metadata) error {
		m.Set("cloud_ruid", ruid)
		m.Set("cloud_auth", strings.Repeat("00", 32))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Simulate a later authenticated claim from the original figurine.
	request := httptest.NewRequest("GET", "/v1/claim/"+ruid, nil)
	request.Header.Set("Authorization", "BD "+strings.Repeat("AB", 32))
	// Commit the read to the selected box, as a device request does.
	if err := s.Content.Update(snap, ruid, func(m content.Metadata) error { updateTagAuth(m, request, snap, ruid, false); return nil }); err != nil {
		t.Fatal(err)
	}
	mock := &mockUpstream{}
	s.Cloud = mock
	reply := perform(s, "GET", "/content/download/97763C1A/500304E0?overlay="+id, nil, false)
	if reply.Code != 200 || len(mock.requests) != 1 {
		t.Fatal(reply.Code, len(mock.requests))
	}
	if mock.requests[0].Path != "/v2/content/"+ruid || mock.requests[0].Header.Get("Authorization") != "BD "+strings.Repeat("AB", 32) {
		t.Fatal("wrong saved tag identity forwarded")
	}
	metadata, _ := s.Content.Metadata(snap, ruid)
	if metadata.Bool("nocloud") || cloudAuthState(metadata) != "present" {
		t.Fatal("browser download changed the tag identity or policy")
	}
}
