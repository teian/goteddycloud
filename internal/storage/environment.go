// SPDX-License-Identifier: GPL-2.0-only
package storage

import (
	"context"
	"fmt"
	"os"
	"strconv"
)

// FromEnvironment selects a deployment backend before persisted settings load.
func FromEnvironment(ctx context.Context, base string) (Backend, error) {
	switch name := os.Getenv("TEDDYCLOUD_STORAGE"); name {
	case "", "filesystem":
		return Filesystem{}, nil
	case "s3":
		pathStyle := false
		if value := os.Getenv("TEDDYCLOUD_S3_PATH_STYLE"); value != "" {
			v, err := strconv.ParseBool(value)
			if err != nil {
				return nil, fmt.Errorf("invalid TEDDYCLOUD_S3_PATH_STYLE")
			}
			pathStyle = v
		}
		return NewS3(ctx, base, S3Options{Bucket: os.Getenv("TEDDYCLOUD_S3_BUCKET"), Prefix: os.Getenv("TEDDYCLOUD_S3_PREFIX"), Endpoint: os.Getenv("TEDDYCLOUD_S3_ENDPOINT"), Region: os.Getenv("AWS_REGION"), PathStyle: pathStyle, TempDir: os.Getenv("TEDDYCLOUD_STORAGE_TEMP_DIR")})
	default:
		return nil, fmt.Errorf("unknown storage backend %q", name)
	}
}
