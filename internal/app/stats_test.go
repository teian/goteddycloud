// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"io"
	"sync/atomic"
	"testing"
)

type failedCloudBody struct{}

func (failedCloudBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (failedCloudBody) Close() error             { return nil }
func TestCloudBodyReadFailureCountedOnce(t *testing.T) {
	var failures atomic.Uint64
	body := &cloudBodyCounter{ReadCloser: failedCloudBody{}, failed: &failures}
	for range 3 {
		if _, err := body.Read(make([]byte, 8)); err != io.ErrUnexpectedEOF {
			t.Fatal(err)
		}
	}
	if failures.Load() != 1 {
		t.Fatal("duplicate failure count", failures.Load())
	}
}
