// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"goteddycloud/internal/config"
	"goteddycloud/internal/content"
	"goteddycloud/internal/runtime"
	"goteddycloud/internal/taf"
)

func (s *Server) v3ContentMeta(w http.ResponseWriter, r *http.Request, id string, snap config.Snapshot) {
	ruid, ok := ruidAt(r.URL.Path, 17)
	if !ok {
		response(w, 404, "", nil)
		return
	}
	if err := s.Content.UpdateContext(r.Context(), snap, ruid, func(m content.Metadata) error {
		m.Set("hide", false)
		updateTagAuth(m, r, snap, ruid, true)
		return nil
	}); err != nil {
		response(w, 500, "", nil)
		return
	}
	metadata, err := s.Content.Metadata(snap, ruid)
	if err != nil {
		response(w, 500, "", nil)
		return
	}
	if runtime.RequestPeer(r.Context()).Box {
		s.tagPresented(r.Context(), snap, id, ruid)
	}
	defer s.tagUpdated(id, ruid)
	_ = s.Content.EnsurePlaylist(r.Context(), snap, s.Settings.Snapshot(""), ruid)
	file, header, err := s.Content.OpenContext(r.Context(), snap, s.Settings.Snapshot(""), ruid)
	if err == nil {
		defer file.Close()
		tracks := header.TrackPageNums
		chapters := len(tracks)
		if chapters == 0 {
			chapters = 1
		}
		if chapters > 99 {
			response(w, 400, "", nil)
			return
		}
		items := make([]map[string]any, 0, chapters)
		for chapter := 0; chapter < chapters; chapter++ {
			size := header.GetNumBytes()
			if chapters > 1 {
				start := uint64(tracks[chapter])
				end := header.GetNumBytes() / taf.BlockSize
				if chapter+1 < chapters {
					end = uint64(tracks[chapter+1])
				}
				if start >= end || end > header.GetNumBytes()/taf.BlockSize {
					response(w, 400, "", nil)
					return
				}
				size = (end - start) * taf.BlockSize
				if chapter > 0 {
					size += 512
				}
			}
			items = append(items, map[string]any{"type": "audio", "name": "teddycloud_" + twoDigits(chapter) + "_" + ruid, "auth": "", "analytics": map[string]any{}, "fileSize": size})
		}
		resume := "resume"
		if metadata.Bool("live") {
			resume = "alwaysReset"
		}
		data, _ := json.Marshal(map[string]any{"version": header.GetAudioId(), "contentType": "content_tonie", "tonieType": "content", "resumeBehavior": resume, "tonieSalesId": metadata.String("tonie_model"), "content": items})
		response(w, 200, "application/json; charset=utf-8", data)
		return
	}
	if enabled(snap, "enableV3ContentMeta") && (!metadata.Bool("nocloud") || metadata.Bool("cloud_override")) {
		path, headers := cloudTagRequest(r, metadata, ruid)
		if s.cloudResponse(w, r, snap, path, headers, nil) {
			return
		}
	}
	response(w, 404, "", nil)
}
func twoDigits(number int) string {
	if number < 10 {
		return "0" + strconv.Itoa(number)
	}
	return strconv.Itoa(number)
}

// A later chapter repeats the original 512-byte Ogg headers before its pages.
type chapterReader struct{ prefix, payload *io.SectionReader }

func (r chapterReader) ReadAt(data []byte, offset int64) (int, error) {
	if offset < 0 {
		return 0, io.EOF
	}
	count := 0
	if offset < r.prefix.Size() {
		size := int64(len(data))
		if size > r.prefix.Size()-offset {
			size = r.prefix.Size() - offset
		}
		n, err := r.prefix.ReadAt(data[:size], offset)
		count += n
		if err != nil {
			return count, err
		}
		if count == len(data) {
			return count, nil
		}
		offset = r.prefix.Size()
	}
	n, err := r.payload.ReadAt(data[count:], offset-r.prefix.Size())
	return count + n, err
}
func (s *Server) v3Chapter(w http.ResponseWriter, r *http.Request, snap config.Snapshot) {
	if !strings.HasPrefix(r.URL.Path, "/v3/chapter/teddycloud_") {
		if enabled(snap, "enableV3Chapter") && s.cloudResponse(w, r, snap, r.URL.Path, cloudHeaders(r.Header), nil) {
			return
		}
		response(w, 404, "", nil)
		return
	}
	ruid, ok := ruidAt(r.URL.Path, 26)
	if !ok {
		response(w, 404, "", nil)
		return
	}
	chapter, err := strconv.Atoi(r.URL.Path[23:25])
	if err != nil || chapter < 0 {
		response(w, 404, "", nil)
		return
	}
	if err := s.Content.UpdateContext(r.Context(), snap, ruid, func(m content.Metadata) error {
		m.Set("hide", false)
		updateTagAuth(m, r, snap, ruid, true)
		return nil
	}); err != nil {
		response(w, 500, "", nil)
		return
	}
	_ = s.Content.EnsurePlaylist(r.Context(), snap, s.Settings.Snapshot(""), ruid)
	file, header, err := s.Content.OpenContext(r.Context(), snap, s.Settings.Snapshot(""), ruid)
	if err != nil {
		metadata, metadataErr := s.Content.Metadata(snap, ruid)
		// The reference's generated chapter path tests cloud.enabled directly.
		if metadataErr == nil && snap.Bool("cloud.enabled") && (!metadata.Bool("nocloud") || metadata.Bool("cloud_override")) {
			path, headers := cloudTagRequest(r, metadata, ruid)
			if s.cloudResponse(w, r, snap, path, headers, nil) {
				return
			}
		}
		response(w, 404, "", nil)
		return
	}
	defer file.Close()
	s.markLibraryListened(r.Context(), snap, ruid)
	if chapter >= len(header.TrackPageNums) {
		response(w, 404, "", nil)
		return
	}
	start := int64(header.TrackPageNums[chapter]) * taf.BlockSize
	end := int64(header.GetNumBytes())
	if chapter+1 < len(header.TrackPageNums) {
		end = int64(header.TrackPageNums[chapter+1]) * taf.BlockSize
	}
	if start < 0 || end <= start || uint64(end) > header.GetNumBytes() {
		response(w, 404, "", nil)
		return
	}
	payload := io.NewSectionReader(file, taf.BlockSize+start, end-start)
	var reader io.ReaderAt = payload
	size := end - start
	if chapter > 0 {
		reader = chapterReader{prefix: io.NewSectionReader(file, taf.BlockSize, 512), payload: payload}
		size += 512
	}
	serveRangeFile(w, r, reader, 0, size, "application/octet-stream")
}
