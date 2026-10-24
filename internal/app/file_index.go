// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"goteddycloud/internal/catalog"
	"goteddycloud/internal/content"
	"goteddycloud/internal/protocol"
	"goteddycloud/internal/storage"
	"goteddycloud/internal/taf"
)

const indexLimit = 10000

func jsonReply(w http.ResponseWriter, kind string, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		response(w, 500, "", nil)
		return
	}
	response(w, 200, kind, data)
}
func tonieDescription(entry catalog.Entry, fallback string) map[string]any {
	out := map[string]any{"tracks": []string{}, "model": fallback, "series": "", "episode": "", "picture": "/img_unknown.png"}
	if entry == nil {
		return out
	}
	for key, source := range map[string]string{"model": "model", "series": "series", "episode": "episodes", "picture": "pic", "language": "language"} {
		out[key] = entry.String(source)
	}
	var tracks []string
	if json.Unmarshal(entry["tracks"], &tracks) == nil && tracks != nil {
		out["tracks"] = tracks
	}
	return out
}
func trackSeconds(f io.ReaderAt, h *protocol.TonieboxAudioFileHeader) []uint32 {
	result := make([]uint32, 0, len(h.TrackPageNums))
	var correction uint64
	for i, page := range h.TrackPageNums {
		offset := int64(4096) + int64(page)*4096
		if i == 0 {
			offset += 0x200
		}
		var b [14]byte
		if _, err := f.ReadAt(b[:], offset); err != nil || string(b[:4]) != "OggS" || b[4] != 0 || b[5] != 0 {
			return []uint32{}
		}
		granule := binary.LittleEndian.Uint64(b[6:])
		result = append(result, uint32((granule-correction)/48000))
		if i == 0 {
			correction = granule
		}
	}
	return result
}

// The reference measures to the first page in the final TAF block, relative
// to the first audio page. Keep that definition instead of decoder duration.
func lengthSeconds(f io.ReaderAt, h *protocol.TonieboxAudioFileHeader) uint64 {
	if len(h.TrackPageNums) == 0 || h.GetNumBytes() < 4096 || len(trackSeconds(f, h)) != len(h.TrackPageNums) {
		return 0
	}
	var first, last [14]byte
	if _, err := f.ReadAt(first[:], 4096+int64(h.TrackPageNums[0])*4096+0x200); err != nil || string(first[:4]) != "OggS" {
		return 0
	}
	offset := int64(4096) + int64((h.GetNumBytes()-1)/4096)*4096
	if _, err := f.ReadAt(last[:], offset); err != nil || string(last[:4]) != "OggS" {
		return 0
	}
	correction, end := binary.LittleEndian.Uint64(first[6:]), binary.LittleEndian.Uint64(last[6:])
	if end < correction {
		return 0
	}
	return (end - correction) / 48000
}
func fileMetadata(root *storage.Root, name string) content.Metadata {
	if n := strings.Index(name, ".json"); n >= 0 {
		name = name[:n]
	}
	m := content.Metadata{}
	f, err := root.Open(name + ".json")
	if err != nil {
		return m
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return m
	}
	if json.Unmarshal(data, &m) != nil || m == nil {
		return content.Metadata{}
	}
	return m
}
func tagPasswordPresent(token []byte) bool {
	if len(token) != 32 {
		return false
	}
	for _, value := range token {
		if value != 0 {
			return true
		}
	}
	return false
}

// Presence of a nonempty password is not proof that Tonies will accept it.
func cloudAuthState(m content.Metadata) string {
	token, err := hex.DecodeString(m.String("cloud_auth"))
	if err != nil || len(token) != 32 {
		return "missing"
	}
	if !tagPasswordPresent(token) {
		return "empty"
	}
	return "present"
}
func hasCloudAuth(m content.Metadata) bool { return cloudAuthState(m) == "present" }

func (s *Server) fileIndexAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != "GET" || !strings.HasPrefix(r.URL.Path, "/api/fileIndex") {
		return false
	}
	v2 := strings.HasPrefix(r.URL.Path, "/api/fileIndexV2")
	q := r.URL.Query()
	snap := s.Settings.Snapshot(q.Get("overlay"))
	root, prefix, err := s.Content.ManagedRoot(snap, q.Get("special"))
	if err != nil {
		response(w, 500, "", nil)
		return true
	}
	folder, err := managedPath(prefix, q.Get("path"))
	if err != nil {
		response(w, 400, "", nil)
		return true
	}
	rows := make([]map[string]any, 0)
	dir, err := root.Open(folder)
	if err != nil {
		if !v2 {
			jsonReply(w, "text/json", map[string]any{"files": rows})
		} else {
			response(w, 500, "", nil)
		}
		return true
	}
	defer dir.Close()
	entries, err := dir.ReadDir(indexLimit + 1)
	if err != nil && err != io.EOF || len(entries) > indexLimit {
		response(w, 413, "", nil)
		return true
	}
	if v2 && folder != "." && folder != prefix {
		parent := filepath.Dir(folder)
		if info, statErr := root.Stat(parent); statErr == nil {
			entries = append([]os.DirEntry{parentEntry{fs.FileInfoToDirEntry(info)}}, entries...)
		}
	}
	// Stable sorting is provided by the UI. Keep native directory order as C does.
	budget := indexLimit
	var linkedModels map[string]string
	linkedModelsLoaded := false
	for _, entry := range entries {
		if r.Context().Err() != nil {
			return true
		}
		name := filepath.Join(folder, entry.Name())
		info, err := root.Stat(name)
		if err != nil {
			continue
		}
		if v2 && !info.IsDir() && len(entry.Name()) > 5 && strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			if sibling, err := root.Stat(name[:len(name)-5]); err == nil && sibling.Mode().IsRegular() {
				continue
			}
		}
		row := map[string]any{"name": entry.Name(), "size": info.Size()}
		if v2 {
			row["date"] = info.ModTime().Unix()
			row["isDir"] = info.IsDir()
		} else {
			row["date"] = info.ModTime().UTC().Format(" 2006-01-02,  15:04:05")
			row["isDirectory"] = info.IsDir()
			row["desc"] = ""
		}
		if v2 && q.Get("special") == "custom_img" {
			rows = append(rows, row)
			continue
		}
		var item catalog.Entry
		metadata := fileMetadata(root, name)
		var file storage.File
		var header *protocol.TonieboxAudioFileHeader
		parts := strings.Split(filepath.ToSlash(name), "/")
		uid := ""
		if q.Get("special") != "library" && q.Get("special") != "custom_img" && len(parts) == 2 && len(parts[0]) == 8 && len(parts[1]) == 8 {
			if _, uidErr := content.UIDPath(parts[0] + parts[1]); uidErr == nil {
				uid = parts[0] + parts[1]
			}
		}
		if uid != "" {
			file, header, _ = s.Content.InspectContext(r.Context(), snap, s.Settings.Snapshot(""), uid)
		} else if !info.IsDir() && !strings.Contains(name, ".json") {
			file, err = root.Open(name)
			if err == nil {
				header, err = taf.ReadHeader(file)
				if err == nil && snap.Bool("core.full_taf_validation") {
					file.Seek(0, 0)
					header, err = taf.Validate(file)
				}
				if err != nil {
					header = nil
				}
			}
		}
		if header != nil {
			if v2 {
				row["tafHeader"] = map[string]any{"audioId": header.GetAudioId(), "sha1Hash": hex.EncodeToString(header.Sha1Hash), "size": header.GetNumBytes(), "valid": true, "trackSeconds": trackSeconds(file, header)}
				if length := lengthSeconds(file, header); length > 0 {
					row["tafHeader"].(map[string]any)["lengthSeconds"] = length
				}
				row["listened"] = fileMetadata(root, name).Bool("listened")
			} else {
				row["desc"] = fmt.Sprintf("TAF:%08X:%s:%d:%d", header.GetAudioId(), hex.EncodeToString(header.Sha1Hash), header.GetNumBytes(), len(header.TrackPageNums))
			}
			item = s.Catalog.ByAudio(header.GetAudioId(), header.Sha1Hash)
			if item == nil {
				item = s.Catalog.ByModel(metadata.String("tonie_model"))
			}
			if item == nil && q.Get("special") == "library" {
				if !linkedModelsLoaded {
					linkedModels, _ = s.linkedLibraryModels(r.Context(), snap)
					linkedModelsLoaded = true
				}
				item = s.Catalog.ByModel(linkedModels[filepath.Clean(name)])
			}
		} else if info.IsDir() {
			sub, err := root.Open(name)
			if err == nil {
				children, _ := sub.ReadDir(budget + 1)
				sub.Close()
				if len(children) > budget {
					if file != nil {
						file.Close()
					}
					response(w, 413, "", nil)
					return true
				}
				budget -= len(children)
				if v2 && len(children) > 0 {
					row["hide"] = false
				}
				for _, child := range children {
					metadata = fileMetadata(root, filepath.Join(name, child.Name()))
					if v2 {
						row["hide"] = metadata.Bool("hide")
					}
					item = s.Catalog.ByModel(metadata.String("tonie_model"))
					if item != nil {
						break
					}
				}
			}
		} else {
			item = s.Catalog.ByModel(metadata.String("tonie_model"))
			if v2 {
				row["hide"] = metadata.Bool("hide")
				row["listened"] = metadata.Bool("listened")
			}
			if hasCloudAuth(metadata) {
				row["has_cloud_auth"] = true
			}
		}
		if file != nil {
			file.Close()
		}
		if item != nil {
			row["tonieInfo"] = s.tonieDescription(r.Context(), item, "")
		}
		rows = append(rows, row)
	}
	jsonReply(w, "text/json", map[string]any{"files": rows})
	return true
}

type parentEntry struct{ os.DirEntry }

func (parentEntry) Name() string { return ".." }
