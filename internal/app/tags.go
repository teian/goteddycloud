// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"goteddycloud/internal/catalog"
	"goteddycloud/internal/config"
	"goteddycloud/internal/content"
)

func (s *Server) tagsAPI(w http.ResponseWriter, r *http.Request) bool {
	p := r.URL.Path
	q := r.URL.Query()
	id := q.Get("overlay")
	if r.Method == "POST" && strings.HasPrefix(p, "/api/assignUnknown") {
		snap := s.Settings.Snapshot(id)
		root, prefix, err := s.Content.ManagedRoot(snap, q.Get("special"))
		if err != nil {
			response(w, 500, "", nil)
			return true
		}
		name, err := managedPath(prefix, q.Get("path"))
		if err != nil {
			response(w, 400, "", nil)
			return true
		}
		path := filepath.Join(root.Name(), name)
		s.assignmentMu.Lock()
		err = s.Settings.SetRuntime("", map[string]string{"internal.assign_unknown": path})
		if err == nil {
			s.assignmentVersion++
		}
		s.assignmentMu.Unlock()
		if err != nil {
			response(w, 500, "", nil)
		} else {
			response(w, 200, "application/octet-stream", []byte("OK"))
		}
		return true
	}
	if r.Method != "GET" {
		return false
	}
	switch {
	case strings.HasPrefix(p, "/api/getBoxes"):
		boxes := make([]map[string]string, 0)
		for _, box := range s.Settings.BoxIDs() {
			settings := s.Settings.Snapshot(box)
			boxes = append(boxes, map[string]string{"ID": box, "commonName": settings.String("commonName"), "boxName": settings.String("boxName"), "boxModel": settings.String("boxModel")})
		}
		jsonReply(w, "text/json", map[string]any{"boxes": boxes})
	case strings.HasPrefix(p, "/api/getTagInfo"):
		snap := s.Settings.Snapshot(id)
		info, err := s.tagInfo(r.Context(), snap, id, q.Get("ruid"))
		if err != nil {
			referenceNotFound(w)
		} else {
			jsonReply(w, "text/json", map[string]any{"tagInfo": info})
		}
	case strings.HasPrefix(p, "/api/getTagIndex"):
		snap := s.Settings.Snapshot(id)
		root, prefix, err := s.Content.ManagedRoot(snap, q.Get("special"))
		if err != nil {
			response(w, 500, "", nil)
			return true
		}
		dir, err := root.Open(filepath.Join(prefix, "."))
		if err != nil {
			response(w, 500, "", nil)
			return true
		}
		defer dir.Close()
		entries, err := dir.ReadDir(indexLimit + 1)
		if err != nil && err != io.EOF || len(entries) > indexLimit {
			response(w, 413, "", nil)
			return true
		}
		tags := make([]map[string]any, 0)
		budget := indexLimit
		for _, entry := range entries {
			if r.Context().Err() != nil {
				return true
			}
			if !entry.IsDir() || !hexPrefix(entry.Name(), 8) || len(entry.Name()) != 8 {
				continue
			}
			sub, err := root.Open(filepath.Join(prefix, entry.Name()))
			if err != nil {
				continue
			}
			children, _ := sub.ReadDir(budget + 1)
			sub.Close()
			if len(children) > budget {
				response(w, 413, "", nil)
				return true
			}
			budget -= len(children)
			for _, child := range children {
				if child.IsDir() || !hexPrefix(child.Name(), 8) {
					continue
				}
				uid := strings.ToLower(entry.Name() + child.Name()[:8])
				if info, err := s.tagInfo(r.Context(), snap, id, uid); err == nil {
					tags = append(tags, info)
					break
				}
			}
		}
		jsonReply(w, "text/json", map[string]any{"tags": tags})
	case strings.HasPrefix(p, "/api/tonieboxesCustomJson"):
		root, err := s.Settings.Storage().OpenRoot(s.Settings.String("", "internal.configdirfull"))
		if err != nil {
			response(w, 500, "", nil)
			return true
		}
		defer root.Close()
		unlock, err := root.LockContext(r.Context(), "tonieboxes.custom.json")
		if err != nil {
			return true
		}
		defer unlock()
		data, err := root.ReadFile("tonieboxes.custom.json")
		if os.IsNotExist(err) {
			data = []byte("[]")
			err = root.WriteAtomic("tonieboxes.custom.json", data, 0600)
		}
		if err != nil {
			response(w, 500, "", nil)
		} else {
			response(w, 200, "application/octet-stream", data)
		}
	case strings.HasPrefix(p, "/api/tonieboxesJson"):
		s.tonieboxesJSON(w, r)
	case strings.HasPrefix(p, "/api/trigger"):
		message := "FAILED"
		var err error
		switch p {
		case "/api/triggerReloadConfig":
			err = s.Settings.Reload()
			message = "OK"
		case "/api/triggerWriteConfig":
			err = s.Settings.Persist()
			message = "OK"
		case "/api/triggerExit", "/api/triggerRestart":
			if s.Shutdown != nil {
				message = "OK"
				response(w, 200, "text/plain", []byte(message))
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				s.Shutdown(p == "/api/triggerRestart")
				return true
			}
		}
		if err != nil {
			message = "FAILED"
		}
		response(w, 200, "text/plain", []byte(message))
	default:
		return false
	}
	return true
}
func hexPrefix(value string, n int) bool {
	if len(value) < n {
		return false
	}
	_, err := hex.DecodeString(value[:n])
	return err == nil
}

func (s *Server) tagInfo(ctx context.Context, snap config.Snapshot, id, uid string) (map[string]any, error) {
	name, err := content.UIDPath(uid)
	if err != nil {
		return nil, err
	}
	root, _, err := s.Content.ManagedRoot(snap, "")
	if err != nil {
		return nil, err
	}
	if _, err := root.Stat(name + ".json"); err != nil {
		return nil, err
	}
	m, err := s.Content.Metadata(snap, uid)
	if err != nil {
		return nil, err
	}
	file, header, _ := s.Content.InspectContext(ctx, snap, s.Settings.Snapshot(""), uid)
	exists := file != nil
	if file != nil {
		defer file.Close()
	}
	tracks := []uint32{}
	sourceModel := ""
	if header != nil {
		tracks = trackSeconds(file, header)
		if item := s.Catalog.ByAudio(header.GetAudioId(), header.Sha1Hash); item != nil {
			sourceModel = item.String("model")
		}
	}
	if m.String("tonie_model") == "" && sourceModel != "" {
		if err := s.Content.UpdateContext(ctx, snap, uid, func(current content.Metadata) error {
			if current.String("tonie_model") == "" {
				current.Set("tonie_model", sourceModel)
			}
			return nil
		}); err != nil {
			return nil, err
		}
		m, _ = s.Content.Metadata(snap, uid)
	}
	bytes, _ := hex.DecodeString(uid)
	parts := make([]string, 8)
	for i := range parts {
		parts[i] = fmt.Sprintf("%02X", bytes[7-i])
	}
	kind := "tag"
	system := strings.HasPrefix(uid, "0000000")
	if system {
		kind = "system"
	}
	download := "/content/download/" + name + "?overlay=" + url.QueryEscape(id)
	info := map[string]any{"ruid": uid, "uid": strings.Join(parts, ":"), "type": kind, "valid": header != nil, "exists": exists, "live": m.Bool("live"), "nocloud": m.Bool("nocloud"), "hasCloudAuth": hasCloudAuth(m) && !m.Bool("cloud_override"), "hide": m.Bool("hide"), "claimed": m.Bool("claimed"), "source": m.String("source"), "trackSeconds": tracks, "audioUrl": download + "&skip_header=true", "tonieInfo": s.tonieDescription(ctx, s.Catalog.ByModel(m.String("tonie_model")), m.String("tonie_model"))}
	if header != nil {
		if length := lengthSeconds(file, header); length > 0 {
			info["lengthSeconds"] = length
		}
	}
	if !exists && !m.Bool("nocloud") {
		info["downloadTriggerUrl"] = ""
		if system || hasCloudAuth(m) {
			info["downloadTriggerUrl"] = download
		}
	}
	if exists && sourceModel != m.String("tonie_model") && (s.Catalog.ByModel(sourceModel) != nil || s.Catalog.ByModel(m.String("tonie_model")) != nil) {
		info["sourceInfo"] = s.tonieDescription(ctx, s.Catalog.ByModel(sourceModel), sourceModel)
	}
	return info, nil
}

func (s *Server) tonieboxesJSON(w http.ResponseWriter, r *http.Request) {
	root, err := s.Settings.Storage().OpenRoot(s.Settings.String("", "internal.configdirfull"))
	if err != nil {
		response(w, 500, "", nil)
		return
	}
	defer root.Close()
	f, err := root.Open("tonieboxes.json")
	if err != nil {
		response(w, 500, "", nil)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	var entries []map[string]json.RawMessage
	if err != nil || len(data) > 4<<20 || json.Unmarshal(data, &entries) != nil || entries == nil {
		response(w, 500, "", nil)
		return
	}
	result := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		row := map[string]any{}
		for _, key := range []string{"id", "name", "img_src"} {
			var value string
			_ = json.Unmarshal(e[key], &value)
			row[key] = value
		}
		row["img_src"] = s.imageURL(r.Context(), row["img_src"].(string), s.Settings.Snapshot("").Bool("tonie_json.cache_preload"))
		var crop []json.RawMessage
		if json.Unmarshal(e["crop"], &crop) == nil && crop != nil {
			numbers := [3]float64{}
			for i := 0; i < len(crop) && i < 3; i++ {
				_ = json.Unmarshal(crop[i], &numbers[i])
			}
			row["crop"] = numbers
		}
		result = append(result, row)
	}
	jsonReply(w, "application/json; charset=utf-8", result)
}

func (s *Server) assignUnknown(r *http.Request, snap config.Snapshot, uid string) {
	if s.Settings.Snapshot("").String("internal.assign_unknown") == "" {
		return
	}
	release, err := s.assignmentWork.Lock(r.Context())
	if err != nil {
		return
	}
	defer release()
	source, version := s.pendingAssignment()
	if source == "" {
		return
	}
	file, _, _ := s.Content.InspectContext(r.Context(), snap, s.Settings.Snapshot(""), uid)
	exists := file != nil
	if file != nil {
		file.Close()
	}
	reversed := ""
	for i := len(uid) - 2; i >= 0; i -= 2 {
		reversed += uid[i : i+2]
	}
	flex := snap.Bool("core.flex_enabled") && strings.EqualFold(reversed, snap.String("core.flex_uid"))
	if exists && !flex {
		return
	}
	// The reference consumes the pending assignment after an eligible attempt,
	// including missing or malformed sources. A known tag leaves it pending.
	defer s.consumeAssignment(source, version)
	_ = s.Content.Assign(r.Context(), snap, s.Settings.Snapshot(""), uid, source, flex)
}

func (s *Server) tonieDescription(ctx context.Context, entry catalog.Entry, fallback string) map[string]any {
	result := tonieDescription(entry, fallback)
	if entry != nil {
		result["picture"] = s.registerImage(ctx, entry.String("pic"), s.Settings.Snapshot("").Bool("tonie_json.cache_preload"), false)
	}
	return result
}

// pendingAssignment snapshots a reservation without holding a lock during I/O.
func (s *Server) pendingAssignment() (string, uint64) {
	s.assignmentMu.Lock()
	defer s.assignmentMu.Unlock()
	return s.Settings.String("", "internal.assign_unknown"), s.assignmentVersion
}
func (s *Server) consumeAssignment(source string, version uint64) {
	s.assignmentMu.Lock()
	defer s.assignmentMu.Unlock()
	if s.assignmentVersion == version && s.Settings.String("", "internal.assign_unknown") == source {
		_ = s.Settings.SetRuntime("", map[string]string{"internal.assign_unknown": ""})
	}
}
