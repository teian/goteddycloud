// SPDX-License-Identifier: GPL-2.0-only
package s3api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCredentialPrecedenceAndProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(path, []byte("[default]\naws_access_key_id=default-key\naws_secret_access_key=default-secret\n[alternate]\naws_access_key_id=profile-key\naws_secret_access_key=profile-secret\naws_session_token=profile-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", path)
	t.Setenv("AWS_PROFILE", "alternate")
	credentials, err := LoadCredentials()
	if err != nil || credentials.AccessKey != "profile-key" || credentials.Token != "profile-token" {
		t.Fatal(credentials.AccessKey, err)
	}
	t.Setenv("AWS_ACCESS_KEY_ID", "environment-key")
	if _, err = LoadCredentials(); err == nil {
		t.Fatal("partial credentials accepted")
	}
	t.Setenv("AWS_SECRET_ACCESS_KEY", "environment-secret")
	credentials, err = LoadCredentials()
	if err != nil || credentials.AccessKey != "environment-key" {
		t.Fatal(err)
	}
}
func TestClientRejectsRedirectsAndEmbeddedCopyErrors(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "synthetic-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "synthetic-secret")
	var redirected bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected = true; w.WriteHeader(200) }))
	defer other.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bucket/copy" {
			w.Write([]byte(`<Error><Code>InternalError</Code><Message>private diagnostic</Message></Error>`))
			return
		}
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client, err := New(Options{Endpoint: server.URL, Bucket: "bucket", Region: "us-east-1", PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Delete(context.Background(), "redirect"); err == nil || redirected {
		t.Fatal("redirect followed", err)
	}
	if err = client.Copy(context.Background(), "source", "copy", ""); err == nil || strings.Contains(err.Error(), "private diagnostic") {
		t.Fatal("copy error", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = client.Put(ctx, "cancelled", strings.NewReader("payload"), 7, nil, false); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled upload", err)
	}
}
