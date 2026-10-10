// SPDX-License-Identifier: GPL-2.0-only
package fixtures

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFixtureRefusesOverwrite(t *testing.T) {
	root := filepath.Join(t.TempDir(), "new")
	if err := Generate(root); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "certs/server/teddy-key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Generate(root); err == nil {
		t.Fatal("existing deployment overwritten")
	}
	after, err := os.ReadFile(filepath.Join(root, "certs/server/teddy-key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("key changed")
	}
	if _, err := os.Stat(filepath.Join(root, Marker)); err != nil {
		t.Fatal(err)
	}
	wav, err := os.ReadFile(filepath.Join(root, "tone1.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if string(wav[:4]) != "RIFF" || int(binary.LittleEndian.Uint32(wav[40:])) != len(wav)-44 {
		t.Fatal("invalid synthetic WAV")
	}
}
func TestMockCloudRecordsIndependentRequests(t *testing.T) {
	cloud := &MockCloud{}
	server := httptest.NewServer(cloud)
	defer server.Close()
	req, err := http.NewRequestWithContext(context.Background(), "GET", server.URL+"/v1/time", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	got := cloud.Records()
	if len(got) != 1 || got[0].Path != "/v1/time" {
		t.Fatal(got)
	}
	got[0].Path = "modified"
	if cloud.Records()[0].Path != "/v1/time" {
		t.Fatal("record copy mutated original")
	}
}
