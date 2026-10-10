// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"goteddycloud/internal/storage"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"goteddycloud/internal/taf"
)

const (
	maxConcurrentTrackExports = 2
	maxExportTracks           = 99
	maxTrackTitleBytes        = 95
	maxTrackExportBytes       = 512 << 20
	oggTrackHeaderBytes       = 512
)

// exportReader checks cancellation between bounded reads, including during a
// single large track. It intentionally exposes no WriterTo fast path.
type exportReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader exportReader) Read(data []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(data)
}
func copyExportSection(ctx context.Context, dst io.Writer, source io.ReaderAt, offset, size int64) error {
	_, err := io.CopyN(dst, exportReader{ctx: ctx, reader: io.NewSectionReader(source, offset, size)}, size)
	return err
}

func trackTitle(v string) string {
	b := []byte(v)
	if len(b) > maxTrackTitleBytes {
		b = b[:maxTrackTitleBytes]
	}
	for i, c := range b {
		if c < 32 || strings.ContainsRune("/\\\"`$", rune(c)) {
			b[i] = '_'
		}
	}
	return string(b)
}
func (s *Server) exportTracks(w http.ResponseWriter, r *http.Request, f storage.File) {
	select {
	case s.trackExports <- struct{}{}:
		defer func() { <-s.trackExports }()
	default:
		audioReply(w, 503, "Track export queue full")
		return
	}
	h, err := taf.ReadHeader(f)
	if err != nil || len(h.TrackPageNums) == 0 {
		audioReply(w, 400, "Invalid TAF")
		return
	}
	selected := strings.FieldsFunc(r.URL.Query().Get("tracks"), func(c rune) bool { return c == ',' || c == ' ' })
	if len(selected) < 1 || len(selected) > maxExportTracks {
		audioReply(w, 400, "Invalid tracks")
		return
	}
	info, err := f.Stat()
	if err != nil {
		response(w, 500, "", nil)
		return
	}
	type track struct {
		name       string
		start, end int64
		headers    bool
	}
	tracks := []track{}
	var total int64
	seen := map[int]bool{}
	var titles []string
	if item := s.Catalog.ByAudio(h.GetAudioId(), nil); item != nil {
		_ = json.Unmarshal(item["tracks"], &titles)
	}
	for _, raw := range selected {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > len(h.TrackPageNums) || n > maxExportTracks || seen[n] {
			audioReply(w, 400, "Invalid tracks")
			return
		}
		seen[n] = true
		chapter := n - 1
		start := int64(taf.BlockSize) + int64(h.TrackPageNums[chapter])*taf.BlockSize
		end := int64(taf.BlockSize) + int64(h.GetNumBytes()/taf.BlockSize)*taf.BlockSize
		if chapter+1 < len(h.TrackPageNums) {
			end = taf.BlockSize + int64(h.TrackPageNums[chapter+1])*taf.BlockSize
		}
		if end > info.Size() {
			end = info.Size()
		}
		if start >= end {
			audioReply(w, 400, "Invalid track bounds")
			return
		}
		name := fmt.Sprintf("%02d", n)
		if chapter < len(titles) {
			if title := trackTitle(titles[chapter]); title != "" {
				name += " " + title
			}
		}
		name += ".ogg"
		tracks = append(tracks, track{name: name, start: start, end: end, headers: chapter > 0})
		total += end - start
		if chapter > 0 {
			total += oggTrackHeaderBytes
		}
	}
	if total > maxTrackExportBytes {
		audioReply(w, 413, "Track export exceeds size limit")
		return
	}
	output, err := os.CreateTemp("", "tc-tracks-*")
	if err != nil {
		response(w, 500, "", nil)
		return
	}
	defer os.Remove(output.Name())
	defer output.Close()
	copyTrack := func(dst io.Writer, t track) error {
		if err := r.Context().Err(); err != nil {
			return err
		}
		if t.headers {
			if err := copyExportSection(r.Context(), dst, f, taf.BlockSize, oggTrackHeaderBytes); err != nil {
				return err
			}
		}
		return copyExportSection(r.Context(), dst, f, t.start, t.end-t.start)
	}
	kind := "audio/ogg"
	if len(tracks) == 1 {
		err = copyTrack(output, tracks[0])
	} else {
		kind = "application/zip"
		archive := zip.NewWriter(output)
		for _, t := range tracks {
			var dst io.Writer
			dst, err = archive.Create(t.name)
			if err != nil {
				break
			}
			if err = copyTrack(dst, t); err != nil {
				break
			}
		}
		closeErr := archive.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
		response(w, 500, "", nil)
		return
	}
	size, err := output.Seek(0, io.SeekEnd)
	if err != nil {
		response(w, 500, "", nil)
		return
	}
	serveRangeFile(w, r, output, 0, size, kind)
}
