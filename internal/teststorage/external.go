// SPDX-License-Identifier: GPL-2.0-only
package teststorage

import (
	"context"
	"crypto/rand"
	"os"
	"testing"
	"time"

	"goteddycloud/internal/s3api"
	"goteddycloud/internal/storage"
)

// external opts into real-provider tests without changing the default unit test
// environment. Each invocation owns a random prefix in an existing test bucket.
// Credentials use the production credential loader; they are never logged.
func external(t testing.TB, base string, injection bool) *storage.S3 {
	t.Helper()
	endpoint := os.Getenv("TEDDYCLOUD_TEST_S3_ENDPOINT")
	if endpoint == "" {
		return nil
	}
	if injection {
		t.Skip("fault injection requires the emulator; run without TEDDYCLOUD_TEST_S3_ENDPOINT")
	}
	bucket := os.Getenv("TEDDYCLOUD_TEST_S3_BUCKET")
	if bucket == "" {
		t.Fatal("TEDDYCLOUD_TEST_S3_BUCKET must name an existing, unversioned test bucket")
	}
	options := s3api.Options{Endpoint: endpoint, Bucket: bucket, Region: os.Getenv("AWS_REGION"), PathStyle: true}
	client, err := s3api.New(options)
	if err != nil {
		t.Fatal(err)
	}
	prefix := "goteddycloud-test/" + rand.Text() + "/"
	t.Log("using external S3 provider with isolated prefix", prefix)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		// Snapshot before deleting: continuation tokens may become invalid when
		// the preceding page is removed on filesystem-backed S3 servers.
		var keys []string
		for token := ""; ; {
			page, err := client.List(ctx, prefix, "", token, 1000)
			if err != nil {
				t.Error("list test objects for cleanup:", err)
				return
			}
			for _, object := range page.Objects {
				keys = append(keys, object.Key)
			}
			if page.NextToken == "" {
				break
			}
			token = page.NextToken
		}
		// Children precede directory markers for filesystem-backed providers.
		for i := len(keys) - 1; i >= 0; i-- {
			if err := client.Delete(ctx, keys[i]); err != nil {
				t.Error("remove test object:", err)
			}
		}
	})
	backend, err := storage.NewS3(context.Background(), base, storage.S3Options{
		Endpoint: endpoint, Bucket: bucket, Prefix: prefix, Region: options.Region,
		PathStyle: true, TempDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	return backend
}
