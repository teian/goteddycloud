// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"google.golang.org/protobuf/proto"

	"goteddycloud/internal/config"
	"goteddycloud/internal/content"
	"goteddycloud/internal/protocol"
)

func (s *Server) v3Simple(w http.ResponseWriter, r *http.Request, snap config.Snapshot, flag, local string) {
	data, err := body(r, 4095)
	if err != nil {
		response(w, 413, "", nil)
		return
	}
	if enabled(snap, flag) && s.cloudResponse(w, r, snap, r.URL.Path, cloudHeaders(r.Header), data) {
		return
	}
	response(w, 200, "application/json; charset=utf-8", []byte(local))
}

func parseV3Freshness(data []byte) (*protocol.TonieFreshnessCheckRequest, error) {
	// Keep the input object's order: it determines the reference marking order.
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, err
	}
	request := new(protocol.TonieFreshnessCheckRequest)
	if object["content"] == nil {
		return request, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(object["content"]))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if token != json.Delim('{') {
		return nil, io.ErrUnexpectedEOF
	}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		ruid, ok := token.(string)
		if !ok {
			return nil, io.ErrUnexpectedEOF
		}
		uid, err := content.UIDFromRUID(ruid)
		if err != nil {
			return nil, err
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		var audioID uint32
		_ = json.Unmarshal(value, &audioID)
		request.TonieInfos = append(request.TonieInfos, &protocol.TonieFCInfo{Uid: proto.Uint64(uid), AudioId: proto.Uint32(audioID)})
	}
	_, err = decoder.Token()
	return request, err
}
func (s *Server) v3Freshness(w http.ResponseWriter, r *http.Request, id string, snap config.Snapshot) {
	data, err := body(r, 4095)
	if err != nil {
		response(w, 413, "", nil)
		return
	}
	request, err := parseV3Freshness(data)
	if err != nil {
		response(w, 400, "", nil)
		return
	}
	reply := s.localFreshnessContext(r.Context(), id, snap, request)
	if enabled(snap, "enableV3FreshnessCheck") {
		cloudContent := map[string]uint32{}
		allowed := map[uint64]bool{}
		for _, info := range request.TonieInfos {
			ruid := content.RUID(info.GetUid())
			metadata, err := s.Content.Metadata(snap, ruid)
			if err != nil || metadata.Bool("nocloud") {
				continue
			}
			audioID := info.GetAudioId()
			if file, header, err := s.Content.OpenContext(r.Context(), snap, s.Settings.Snapshot(""), ruid); err == nil {
				file.Close()
				audioID, _ = normalizedAudioID(header.GetAudioId())
			}
			cloudContent[strings.ToUpper(ruid)] = audioID
			allowed[info.GetUid()] = true
		}
		payload, _ := json.Marshal(map[string]any{"content": cloudContent})
		res, err := s.doCloud(r.Context(), snap, s.Settings.Snapshot(""), r.Method, r.URL.Path, r.URL.RawQuery, cloudHeaders(r.Header), payload)
		if err == nil {
			raw, readErr := io.ReadAll(io.LimitReader(res.Body, 4096))
			res.Body.Close()
			var remote struct {
				Items []string `json:"items"`
			}
			if readErr == nil && len(raw) < 4096 && res.StatusCode == 200 && json.Unmarshal(raw, &remote) == nil {
				marked := map[uint64]bool{}
				for _, uid := range reply.TonieMarked {
					marked[uid] = true
				}
				for _, ruid := range remote.Items {
					uid, err := content.UIDFromRUID(ruid)
					if err == nil && allowed[uid] && !marked[uid] {
						reply.TonieMarked = append(reply.TonieMarked, uid)
						marked[uid] = true
					}
				}
			}
		}
	}
	s.mu.Lock()
	s.freshness[id] = append([]uint64(nil), reply.TonieMarked...)
	s.mu.Unlock()
	items := make([]string, 0, len(reply.TonieMarked))
	for _, uid := range reply.TonieMarked {
		items = append(items, strings.ToUpper(content.RUID(uid)))
	}
	result, _ := json.Marshal(map[string]any{"items": items})
	response(w, 200, "application/json; charset=utf-8", result)
}
