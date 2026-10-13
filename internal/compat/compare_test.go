// SPDX-License-Identifier: GPL-2.0-only
package compat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSemanticComparisonAndMismatches(t *testing.T) {
	for _, tc := range []struct {
		name, kind, left, right string
		ok                      bool
	}{
		{"JSON ordering", "json", `{"a":1,"b":2}`, `{"b":2,"a":1}`, true},
		{"JSON value mismatch", "json", `{"a":1}`, `{"a":2}`, false},
		{"JSON trailing data", "json", `{} {}`, `{}`, false},
		{"binary mismatch", "bytes", "a", "b", false},
		{"SSE JSON whitespace", "sse-json", "event: a\ndata: { \"x\":1 }\n\n", "event: a\ndata: {\"x\":1}\n\n", true},
		{"SSE JSON value mismatch", "sse-json", "event: a\ndata: {\"x\":1}\n\n", "event: a\ndata: {\"x\":2}\n\n", false},
		{"SSE order mismatch", "sse", "event: a\n\nevent: b\n\n", "event: b\n\nevent: a\n\n", false},
		{"epoch tolerance", "unix-time", "1700000000", "1700000001", true},
		{"epoch mismatch", "unix-time", "1700000000", "1700000010", false},
		{"protobuf field order", "protobuf-wire", "\x08\x01\x10\x02", "\x10\x02\x08\x01", true},
		{"protobuf value mismatch", "protobuf-wire", "\x08\x01", "\x08\x02", false},
		{"protobuf truncated", "protobuf-wire", "\x08", "\x08", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Compare(Case{Kind: tc.kind, TimeToleranceSeconds: 2}, Observation{Body: []byte(tc.left)}, Observation{Body: []byte(tc.right)})
			if (err == nil) != tc.ok {
				t.Fatalf("error=%v want success=%v", err, tc.ok)
			}
		})
	}
	if Compare(Case{Kind: "bytes"}, Observation{Status: 200}, Observation{Status: 404}) == nil {
		t.Fatal("status mismatch passed")
	}
	if Compare(Case{Kind: "bytes"}, Observation{Files: map[string]string{"x": "a"}}, Observation{Files: map[string]string{"x": "b"}}) == nil {
		t.Fatal("file mismatch passed")
	}
}
func TestObserveFilesEventsAndLimits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sse" {
			_, _ = w.Write([]byte(":heartbeat\n\nevent: state\ndata: one\n\nevent: state\ndata: two\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "state"), []byte("new state"), 0600); err != nil {
		t.Fatal(err)
	}
	c := Case{Method: "GET", Path: "/", Kind: "json", Headers: []string{"Content-Type"}, Files: []string{"state", "absent"}}
	observed, err := Observe(context.Background(), server.Client(), server.URL, root, c)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Files["state"] == "" || observed.Files["absent"] != "absent" {
		t.Fatal(observed.Files)
	}
	c.Files = []string{"../escape"}
	if _, err := Observe(context.Background(), server.Client(), server.URL, root, c); err == nil {
		t.Fatal("escape accepted")
	}
	c.Files = nil
	c.Path = "/sse"
	c.Kind = "sse"
	c.Events = 2
	observed, err = Observe(context.Background(), server.Client(), server.URL, "", c)
	if err != nil {
		t.Fatal(err)
	}
	if string(observed.Body) != "event: state\ndata: one\n\nevent: state\ndata: two\n\n" {
		t.Fatalf("events: %q", observed.Body)
	}
	c.Path = "https://other.invalid/"
	if _, err := Observe(context.Background(), server.Client(), server.URL, "", c); err == nil {
		t.Fatal("origin escape accepted")
	}
}

func TestMatchingMissingRoutesCannotPassSuccessCase(t *testing.T) {
	if err := Compare(Case{Kind: "bytes", ExpectedStatus: 200}, Observation{Status: 404}, Observation{Status: 404}); err == nil {
		t.Fatal("matching 404 passed success case")
	}
}

func TestSettingsIndexOnlyNormalizesAssertedSecurityText(t *testing.T) {
	left := `{"options":[{"ID":"core.allowNewBox","description":"Allow new boxes to be added, if they try to connect","value":true}]}`
	right := `{"options":[{"ID":"core.allowNewBox","description":"Allow boxes with an already imported, exactly matching client certificate to create an overlay when connecting. Unknown certificates are always rejected.","value":true}]}`
	c := Case{Kind: "settings-index"}
	if err := Compare(c, Observation{Body: []byte(left)}, Observation{Body: []byte(right)}); err != nil {
		t.Fatal(err)
	}
	for _, changed := range []string{strings.Replace(right, `"value":true`, `"value":false`, 1), strings.Replace(right, "Unknown certificates are always rejected.", "Unknown certificates are accepted.", 1)} {
		if Compare(c, Observation{Body: []byte(left)}, Observation{Body: []byte(changed)}) == nil {
			t.Fatal("unexpected setting change normalized away")
		}
	}
}
