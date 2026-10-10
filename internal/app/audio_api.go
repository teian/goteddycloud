// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"goteddycloud/internal/audio"
	"goteddycloud/internal/storage"
	"goteddycloud/internal/taf"
)

const audioUploadLimit = int64(1 << 30)

var audioRequests = make(chan struct{}, 4)

func managedPath(prefix, path string) (string, error) {
	path = strings.TrimLeft(path, "/")
	if path == "" {
		path = "."
	}
	if !filepath.IsLocal(path) || strings.ContainsAny(path, "\\:\x00") {
		return "", errors.New("invalid path")
	}
	for _, part := range strings.Split(path, "/") {
		if part == ".." {
			return "", errors.New("invalid path")
		}
	}
	return filepath.Join(prefix, path), nil
}
func audioReply(w http.ResponseWriter, status int, message string) {
	response(w, status, "text/plain; charset=utf-8", []byte(message))
}

func (s *Server) audioAPI(w http.ResponseWriter, r *http.Request) bool {
	if !strings.EqualFold(r.Method, "POST") {
		return false
	}
	encode := strings.HasPrefix(r.URL.Path, "/api/fileEncode")
	pcm := strings.HasPrefix(r.URL.Path, "/api/pcmUpload")
	upload := strings.HasPrefix(r.URL.Path, "/api/tafUpload")
	if !encode && !pcm && !upload {
		return false
	}
	select {
	case audioRequests <- struct{}{}:
		defer func() { <-audioRequests }()
	default:
		audioReply(w, 503, "Audio queue full")
		return true
	}
	query := r.URL.Query()
	snap := s.Settings.Snapshot(query.Get("overlay"))
	root, prefix, err := s.Content.ManagedRoot(snap, query.Get("special"))
	if err != nil {
		audioReply(w, 500, "Invalid root")
		return true
	}
	temporary, err := os.MkdirTemp("", "goteddycloud-audio-upload-")
	if err != nil {
		audioReply(w, 500, "Temporary storage unavailable")
		return true
	}
	defer os.RemoveAll(temporary)
	if encode {
		s.encodeAPI(w, r, root, prefix, temporary)
		return true
	}
	name := query.Get("name")
	if name == "" {
		if pcm {
			name = "unnamed"
		} else {
			name = "unnamed.taf"
		}
	}
	if name == "." || name == ".." || strings.ContainsAny(name, "/\\:\x00") {
		audioReply(w, 400, "Invalid filename")
		return true
	}
	folder, err := managedPath(prefix, query.Get("path"))
	if err != nil {
		audioReply(w, 400, "Invalid path")
		return true
	}
	info, err := root.Stat(folder)
	if err != nil || !info.IsDir() {
		audioReply(w, 500, fmt.Sprintf("invalid path: '%.200s'", query.Get("path")))
		return true
	}
	target := filepath.Join(folder, name)
	unlock, err := root.LockContext(r.Context(), target)
	if err != nil {
		return true
	}
	defer unlock()
	// Reject oversized uploads before multipart allocation, and allow only one file.
	r.Body = http.MaxBytesReader(w, r.Body, audioUploadLimit+1<<20)
	reader, err := r.MultipartReader()
	if err != nil {
		audioReply(w, 500, "Upload failed")
		return true
	}
	part, err := reader.NextPart()
	if err != nil {
		audioReply(w, 500, "Upload failed")
		return true
	}
	_, disposition, dispositionErr := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
	rawFilename := disposition["filename"]
	if dispositionErr != nil || rawFilename == "" || rawFilename == "." || rawFilename == ".." || strings.ContainsAny(rawFilename, "/\\:\x00") {
		part.Close()
		audioReply(w, 400, "Invalid filename")
		return true
	}
	staged := filepath.Join(temporary, "input")
	err = stageAudio(r.Context(), part, staged, audioUploadLimit)
	part.Close()
	if err != nil {
		audioReply(w, 500, "Upload failed")
		return true
	}
	if next, nextErr := reader.NextPart(); nextErr != io.EOF {
		if next != nil {
			next.Close()
		}
		audioReply(w, 400, "Expected one uploaded file")
		return true
	}
	result := staged
	if pcm {
		id := uint64(0)
		if raw := query.Get("audioId"); raw != "" {
			id, err = strconv.ParseUint(raw, 10, 32)
			if err != nil {
				audioReply(w, 400, "Invalid audioId")
				return true
			}
		}
		result = filepath.Join(temporary, "result.taf")
		if err := audio.ConvertPCM(r.Context(), result, uint32(id), staged); err != nil {
			audioReply(w, 500, "Conversion failed")
			return true
		}
	}
	file, err := os.Open(result)
	if err != nil {
		audioReply(w, 500, "Upload failed")
		return true
	}
	defer file.Close()
	if _, err := taf.Validate(file); err != nil {
		audioReply(w, 500, "TAF validation failed")
		return true
	}
	_, err = file.Seek(0, io.SeekStart)
	if err == nil {
		err = root.WriteReaderAtomic(r.Context(), target, file, 0600, audioUploadLimit)
	}
	if err != nil {
		audioReply(w, 500, "Upload failed")
		return true
	}
	audioReply(w, 200, "OK")
	return true
}

func (s *Server) encodeAPI(w http.ResponseWriter, r *http.Request, root *storage.Root, prefix, temporary string) {
	data, err := body(r, 16384)
	if err != nil {
		audioReply(w, 413, "Request too large")
		return
	}
	form, err := url.ParseQuery(string(data))
	if err != nil {
		audioReply(w, 400, "Invalid form")
		return
	}
	if form.Get("target") == "" {
		audioReply(w, 400, "target missing")
		return
	}
	target, err := managedPath(prefix, form.Get("target"))
	if err != nil || target == prefix || target == "." {
		audioReply(w, 400, "Invalid target")
		return
	}
	unlock, err := root.LockContext(r.Context(), target)
	if err != nil {
		return
	}
	defer unlock()
	if _, err := root.Stat(target); err == nil {
		audioReply(w, 500, fmt.Sprintf("File %s already exists!\r\n", filepath.Join(root.Name(), target)))
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		audioReply(w, 400, "Invalid target")
		return
	}
	sources := form["source"]
	if len(sources) == 0 || len(sources) > 99 {
		audioReply(w, 400, "Expected 1-99 sources")
		return
	}
	remaining := audioUploadLimit
	staged := make([]string, 0, len(sources))
	for index, source := range sources {
		name, err := managedPath(prefix, source)
		if err != nil {
			audioReply(w, 400, "Invalid source")
			return
		}
		input, err := root.Open(name)
		if err != nil {
			audioReply(w, 400, "Source missing")
			return
		}
		info, err := input.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > remaining {
			input.Close()
			audioReply(w, 400, "Invalid source size or type")
			return
		}
		destination := filepath.Join(temporary, fmt.Sprintf("%03d%s", index, filepath.Ext(name)))
		err = stageAudio(r.Context(), input, destination, remaining)
		input.Close()
		if err != nil {
			audioReply(w, 500, "Source staging failed")
			return
		}
		copied, err := os.Stat(destination)
		if err != nil {
			audioReply(w, 500, "Source staging failed")
			return
		}
		remaining -= copied.Size()
		staged = append(staged, destination)
	}
	result := filepath.Join(temporary, "result.taf")
	if err := audio.ConvertWithOptions(r.Context(), result, uint32(s.Now().Unix()-0x50000000), staged, audio.Options{ConfineInput: s.parent != nil}); err != nil {
		audioReply(w, 500, "Conversion failed")
		return
	}
	file, err := os.Open(result)
	if err != nil {
		audioReply(w, 500, "Conversion failed")
		return
	}
	defer file.Close()
	if err := root.WriteReaderAtomic(r.Context(), target, file, 0600, audioUploadLimit); err != nil {
		audioReply(w, 500, "Publication failed")
		return
	}
	audioReply(w, 200, "OK\r\n")
}

type audioContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r audioContextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}
func stageAudio(ctx context.Context, source io.Reader, path string, limit int64) error {
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer output.Close()
	count, err := io.Copy(output, io.LimitReader(audioContextReader{ctx, source}, limit+1))
	if err != nil {
		return err
	}
	if count > limit {
		return errors.New("audio exceeds limit")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return output.Close()
}
