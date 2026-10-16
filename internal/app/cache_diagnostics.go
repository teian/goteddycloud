// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
)

// Storage errors can contain full tag paths, remote URLs or credentials.
// Log only fixed categories, never the original error text.
func cacheFailureReason(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, os.ErrPermission):
		return "permission_denied"
	case errors.Is(err, syscall.ENOSPC):
		return "disk_full"
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		return "incomplete_file"
	case strings.Contains(err.Error(), "TAF payload hash/length mismatch"):
		return "taf_payload_mismatch"
	case strings.Contains(err.Error(), "invalid TAF"), strings.Contains(err.Error(), "TAF protobuf"):
		return "invalid_taf_header"
	case strings.Contains(err.Error(), "chained Ogg streams require explicit track boundaries"):
		return "unsupported_chained_ogg"
	case strings.Contains(err.Error(), "invalid Ogg checksum"):
		return "ogg_checksum_mismatch"
	case strings.Contains(err.Error(), "invalid Ogg serial/sequence"):
		return "ogg_serial_or_sequence"
	case strings.Contains(err.Error(), "missing Ogg beginning"):
		return "ogg_missing_beginning"
	case strings.Contains(err.Error(), "invalid Ogg packet continuation"):
		return "ogg_packet_continuation"
	case strings.Contains(err.Error(), "invalid Ogg integrity"):
		return "invalid_ogg_integrity"
	case strings.Contains(err.Error(), "library audio-ID collision"):
		return "library_audio_id_collision"
	case strings.Contains(err.Error(), "reserved audio ID"):
		return "reserved_audio_id"
	case strings.Contains(err.Error(), "content cache size limit"):
		return "cache_size_limit"
	default:
		return "storage_failure"
	}
}
