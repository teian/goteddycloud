// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"strings"

	"google.golang.org/protobuf/proto"

	"goteddycloud/internal/config"
	"goteddycloud/internal/content"
	"goteddycloud/internal/protocol"
)

type upstream interface {
	Do(context.Context, config.Snapshot, config.Snapshot, string, string, string, http.Header, []byte) (*http.Response, error)
}

func enabled(snap config.Snapshot, flag string) bool {
	return snap.Bool("cloud.enabled") && snap.Bool("cloud."+flag)
}
func (s *Server) cloudResponse(w http.ResponseWriter, r *http.Request, snap config.Snapshot, path string, headers http.Header, data []byte) bool {
	res, err := s.doCloud(r.Context(), snap, s.Settings.Snapshot(""), r.Method, path, r.URL.RawQuery, headers, data)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	for _, key := range []string{"Content-Type", "Content-Length", "Content-Range", "ETag", "Last-Modified", "Cache-Control"} {
		if value := res.Header.Get(key); value != "" {
			w.Header().Set(key, value)
		}
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, res.Body)
	return true
}
func override(m content.Metadata) (string, string, bool) {
	ruid := m.String("cloud_ruid")
	auth := m.String("cloud_auth")
	if !m.Bool("cloud_override") {
		return "", "", false
	}
	if _, err := content.UIDPath(ruid); err != nil {
		return "", "", false
	}
	token, err := hex.DecodeString(auth)
	if err != nil || len(token) != 32 {
		return "", "", false
	}
	return ruid, "BD " + auth, true
}
func cloudTagRequest(r *http.Request, m content.Metadata, ruid string) (string, http.Header) {
	path := r.URL.Path
	headers := r.Header.Clone()
	if _, ok := boxToken(r); !ok {
		headers.Set("Authorization", "BD "+strings.Repeat("0", 64))
	} else {
		headers.Set("Authorization", strings.ToUpper(headers.Get("Authorization")))
	}
	if other, auth, ok := override(m); ok {
		path = strings.Replace(path, ruid, other, 1)
		headers.Set("Authorization", strings.ToUpper(auth))
	}
	return path, headers
}
func (s *Server) cloudFreshness(r *http.Request, snap config.Snapshot, request *protocol.TonieFreshnessCheckRequest, reply *protocol.TonieFreshnessCheckResponse) bool {
	if !enabled(snap, "enableV1FreshnessCheck") {
		return false
	}
	filtered := proto.Clone(request).(*protocol.TonieFreshnessCheckRequest)
	filtered.TonieInfos = nil
	allowed := map[uint64]bool{}
	for _, info := range request.TonieInfos {
		ruid := content.RUID(info.GetUid())
		m, err := s.Content.Metadata(snap, ruid)
		if err != nil || m.Bool("nocloud") {
			continue
		}
		copy := proto.Clone(info).(*protocol.TonieFCInfo)
		f, h, err := s.Content.OpenContext(r.Context(), snap, s.Settings.Snapshot(""), ruid)
		if err == nil {
			f.Close()
			id, _ := normalizedAudioID(h.GetAudioId())
			copy.AudioId = proto.Uint32(id)
		}
		filtered.TonieInfos = append(filtered.TonieInfos, copy)
		allowed[copy.GetUid()] = true
	}
	data, err := proto.Marshal(filtered)
	if err != nil {
		return false
	}
	res, err := s.doCloud(r.Context(), snap, s.Settings.Snapshot(""), "POST", "/v1/freshness-check", r.URL.RawQuery, cloudHeaders(r.Header), data)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	data, err = io.ReadAll(io.LimitReader(res.Body, 4096))
	if err != nil || len(data) >= 4096 || res.StatusCode != 200 {
		return false
	}
	remote := new(protocol.TonieFreshnessCheckResponse)
	if err := proto.Unmarshal(data, remote); err != nil {
		return false
	}
	marked := map[uint64]bool{}
	for _, uid := range reply.TonieMarked {
		marked[uid] = true
	}
	for _, uid := range remote.TonieMarked {
		if allowed[uid] && !marked[uid] {
			reply.TonieMarked = append(reply.TonieMarked, uid)
			marked[uid] = true
		}
	}
	if !snap.Bool("toniebox.overrideCloud") {
		reply.MaxVolSpk = remote.MaxVolSpk
		reply.MaxVolHdp = remote.MaxVolHdp
		reply.SlapEn = remote.SlapEn
		reply.SlapDir = remote.SlapDir
		reply.Field2 = remote.Field2
		reply.Field6 = remote.Field6
		reply.Led = remote.Led
	}
	return true
}

// Non-tag requests use the configured client certificate, without a device tag
// token or web authorization credentials (the reference passes a NULL token).
func cloudHeaders(source http.Header) http.Header {
	headers := source.Clone()
	headers.Del("Authorization")
	return headers
}
