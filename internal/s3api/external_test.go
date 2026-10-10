// SPDX-License-Identifier: GPL-2.0-only
package s3api

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// TestExternalProvider verifies semantics that a permissive emulator cannot
// establish, especially signature validation and conditional operations.
func TestExternalProvider(t *testing.T) {
	endpoint := os.Getenv("TEDDYCLOUD_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("set TEDDYCLOUD_TEST_S3_ENDPOINT and TEDDYCLOUD_TEST_S3_BUCKET")
	}
	options := Options{Endpoint: endpoint, Bucket: os.Getenv("TEDDYCLOUD_TEST_S3_BUCKET"), Region: os.Getenv("AWS_REGION"), PathStyle: true}
	client, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	prefix := "goteddycloud-test/" + rand.Text() + "/"
	source, target := prefix+"日本語 #+%/audio", prefix+"copied #+%"
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		for _, key := range []string{target, source} {
			if err := client.Delete(ctx, key); err != nil {
				t.Error("cleanup:", err)
			}
		}
	})
	put := func(key, value string, exclusive bool) error {
		return client.Put(ctx, key, strings.NewReader(value), int64(len(value)), map[string]string{"test": "preserved"}, exclusive)
	}
	if err := put(source, "original", true); err != nil {
		t.Fatal(err)
	}
	object, err := client.Head(ctx, source)
	if err != nil || object.Size != 8 || object.Metadata["test"] != "preserved" {
		t.Fatalf("metadata: %+v %v", object, err)
	}
	expectStatus := func(err error, status int) {
		t.Helper()
		var failure *Error
		if !errors.As(err, &failure) || failure.Status != status {
			t.Fatalf("want HTTP %d, got %v", status, err)
		}
	}
	expectStatus(put(source, "overwrite", true), 412)
	if err := client.Copy(ctx, source, target, object.ETag); err != nil {
		t.Fatal(err)
	}
	copied, err := client.Head(ctx, target)
	if err != nil || copied.Metadata["test"] != "preserved" {
		t.Fatalf("copy metadata: %+v %v", copied, err)
	}
	reader, err := client.Get(ctx, target, copied.ETag, copied.Version, "bytes=1-3")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || string(data) != "rig" {
		t.Fatalf("range: %q %v", data, err)
	}
	if err := put(source, "replacement", false); err != nil {
		t.Fatal(err)
	}
	reader, err = client.Get(ctx, source, object.ETag, "", "bytes=0-2")
	if reader != nil {
		reader.Close()
	}
	expectStatus(err, 412)
	expectStatus(client.Copy(ctx, source, target, object.ETag), 412)
	// Deliberately corrupt only this client's signing secret. A server that
	// accepts this request has not established authenticated compatibility.
	invalid := *client
	invalid.credentials.SecretKey += "-invalid"
	_, err = invalid.Head(ctx, source)
	expectStatus(err, 403)
}
