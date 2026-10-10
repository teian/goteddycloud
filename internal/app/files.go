// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"

	"goteddycloud/internal/storage"
)

func (s *Server) fileMutationAPI(w http.ResponseWriter, r *http.Request) bool {
	if !strings.EqualFold(r.Method, "POST") {
		return false
	}
	operation := ""
	for _, name := range []string{"fileDelete", "fileMove", "dirDelete", "dirCreate", "migrateContent2Lib"} {
		if strings.HasPrefix(r.URL.Path, "/api/"+name) {
			operation = name
			break
		}
	}
	if operation == "" {
		return false
	}
	data, err := body(r, 4095)
	if err != nil {
		audioReply(w, 413, "Request too large")
		return true
	}
	query := r.URL.Query()
	snap := s.Settings.Snapshot(query.Get("overlay"))
	if operation == "migrateContent2Lib" {
		form, err := url.ParseQuery(string(data))
		ruid := form.Get("ruid")
		if err != nil {
			response(w, 404, "", nil)
			return true
		}
		metadata, err := s.Content.Metadata(snap, ruid)
		if err != nil || metadata.String("source") != "" {
			response(w, 404, "", nil)
			return true
		}
		unlock, err := s.Content.DownloadLock(r.Context(), snap, ruid)
		if err != nil {
			return true
		}
		defer unlock()
		// Recheck after joining a concurrent download or migration.
		metadata, err = s.Content.Metadata(snap, ruid)
		if err != nil || metadata.String("source") != "" {
			response(w, 404, "", nil)
			return true
		}
		if err := s.Content.MigrateToLibrary(r.Context(), snap, ruid, form.Get("libroot") == "true"); err != nil {
			response(w, 404, "", nil)
			return true
		}
		response(w, 200, "application/octet-stream", []byte("OK"))
		return true
	}
	root, prefix, err := s.Content.ManagedRoot(snap, query.Get("special"))
	if err != nil {
		audioReply(w, 500, "Invalid root")
		return true
	}
	root = root.WithContext(r.Context())

	if operation == "fileMove" {
		form, err := url.ParseQuery(string(data))
		if err != nil || form.Get("source") == "" || form.Get("target") == "" {
			audioReply(w, 400, "source and target required")
			return true
		}
		source, sourceErr := managedPath(prefix, form.Get("source"))
		target, targetErr := managedPath(prefix, form.Get("target"))
		if sourceErr != nil || targetErr != nil || source == "." || target == "." || source == prefix || target == prefix {
			audioReply(w, 400, "Invalid path")
			return true
		}
		unlock, err := root.LockContext(r.Context(), source, target)
		if err != nil {
			return true
		}
		defer unlock()
		code := ""
		if _, err := root.Lstat(target); err == nil {
			code = "Not writable [272]"
		} else if !errors.Is(err, os.ErrNotExist) {
			code = "File opening failed [302]"
		}
		info, err := root.Lstat(source)
		if code == "" && err != nil {
			code = "File not found [301]"
		}
		if code == "" && !info.Mode().IsRegular() && !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			code = "Invalid file [300]"
		}
		if code == "" {
			err = root.Rename(source, target)
			if errors.Is(err, syscall.EXDEV) {
				err = copyThenRemove(r, root, source, target)
			}
			if err != nil {
				code = "File opening failed [302]"
			}
		}
		if code != "" {
			audioReply(w, 500, fmt.Sprintf("Error moving file '%s' to '%s', error %s", "/"+strings.TrimLeft(form.Get("source"), "/"), "/"+strings.TrimLeft(form.Get("target"), "/"), code))
			return true
		}
		audioReply(w, 200, "OK")
		return true
	}
	path, err := managedPath(prefix, string(data))
	if err != nil || path == "." || path == prefix {
		audioReply(w, 400, "Invalid path")
		return true
	}
	unlock, err := root.LockContext(r.Context(), path)
	if err != nil {
		return true
	}
	defer unlock()
	if operation == "dirCreate" {
		err = root.Mkdir(path, 0700)
	} else {
		info, statErr := root.Lstat(path)
		err = statErr
		if err == nil {
			if operation == "dirDelete" && !info.IsDir() {
				err = errors.New("not a directory")
			} else if operation == "fileDelete" && info.IsDir() {
				err = errors.New("not a file")
			} else {
				err = root.Remove(path)
			}
		}
	}
	if err != nil {
		verb, kind := "deleting", "file"
		if operation == "dirCreate" {
			verb = "creating"
		}
		if operation != "fileDelete" {
			kind = "directory"
		}
		display := "/" + strings.TrimLeft(string(data), "/")
		if kind == "directory" && !strings.HasSuffix(display, "/") {
			display += "/"
		}
		audioReply(w, 500, fmt.Sprintf("Error %s %s '%s', error Generic error code [1]", verb, kind, display))
		return true
	}
	audioReply(w, 200, "OK")
	return true
}

// Cross-device fallback publishes a complete private copy before deleting source.
// An interrupted copy leaves the source intact and no partial target visible.
func copyThenRemove(r *http.Request, root *storage.Root, source, target string) error {
	file, err := root.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > audioUploadLimit {
		return errors.New("move size/type limit")
	}
	if err := root.WriteReaderAtomic(r.Context(), target, &exactReader{reader: file, remaining: info.Size()}, info.Mode().Perm(), info.Size()); err != nil {
		return err
	}
	file.Close()
	return root.Remove(source)
}

type exactReader struct {
	reader    io.Reader
	remaining int64
}

func (r *exactReader) Read(data []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(data)) > r.remaining {
		data = data[:r.remaining]
	}
	n, err := r.reader.Read(data)
	r.remaining -= int64(n)
	if errors.Is(err, io.EOF) && r.remaining > 0 {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}
