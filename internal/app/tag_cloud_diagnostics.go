// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"encoding/hex"
	"net/http"
	"strings"

	"goteddycloud/internal/content"
	"goteddycloud/internal/runtime"
)

type tagCloudDiagnosticKey struct{}

func tagCloudContext(r *http.Request, ruid string, metadata content.Metadata, headers http.Header) context.Context {
	source := "saved"
	if runtime.RequestPeer(r.Context()).Box {
		source = "device"
	}
	if _, _, ok := override(metadata); ok {
		source = "override"
	}
	savedRUID := "missing"
	if value := metadata.String("cloud_ruid"); value != "" {
		savedRUID = "different"
		if strings.EqualFold(value, ruid) {
			savedRUID = "matches_tag"
		}
	}
	token, present := boxToken(&http.Request{Header: headers})
	matches := present && strings.EqualFold(hex.EncodeToString(token), metadata.String("cloud_auth"))
	return context.WithValue(r.Context(), tagCloudDiagnosticKey{}, []any{
		"auth_source", source, "saved_auth_state", cloudAuthState(metadata),
		"outgoing_auth_matches_saved", matches, "saved_ruid", savedRUID,
		"cloud_override", metadata.Bool("cloud_override"), "tag_cloud_blocked", metadata.Bool("nocloud"),
	})
}
