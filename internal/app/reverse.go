// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type reverseTarget struct {
	host, path, mime string
	ttl              time.Duration
	params, flush    bool
}

var reverseTargets = map[string]reverseTarget{
	"macvendor":          {"api.macvendors.com", "/", "text/plain", 24 * time.Hour, true, false},
	"teddycloud_release": {"api.github.com", "/repos/toniebox-reverse-engineering/teddycloud/releases/latest", "application/json", 24 * time.Hour, false, true},
	"teddycloud_develop": {"api.github.com", "/repos/toniebox-reverse-engineering/teddycloud/commits/develop", "application/json", time.Hour, false, true},
}

func (s *Server) reverseAPI(w http.ResponseWriter, r *http.Request) bool {
	if strings.HasPrefix(r.URL.Path, "/reverseGeneric") {
		s.reverseGeneric(w, r)
		return true
	}
	if !strings.HasPrefix(r.URL.Path, "/reverse") {
		return false
	}
	s.reverseRequests.Add(1)
	if !strings.HasPrefix(r.URL.Path, "/reverse/") {
		response(w, 404, "", nil)
		return true
	}
	// The pinned C handler always issues GET, even when called with POST.
	path, headers := cloudTagRequest(r, nil, "")
	path = strings.TrimPrefix(path, "/reverse")
	snap := s.Settings.Snapshot("")
	res, err := s.doCloud(r.Context(), snap, snap, "GET", path, r.URL.RawQuery, headers, nil)
	if err != nil {
		response(w, 502, "", nil)
		return true
	}
	defer res.Body.Close()
	relayReverse(w, res)
	return true
}

func (s *Server) reverseGeneric(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/reverseGeneric/")
	name, sub, hasSub := strings.Cut(rest, "/")
	target, ok := reverseTargets[name]
	if !strings.HasPrefix(r.URL.Path, "/reverseGeneric/") || !ok {
		response(w, 404, "", nil)
		return
	}
	path := target.path
	if hasSub {
		path = strings.TrimSuffix(path, "/") + "/" + sub
	}
	if target.params {
		path += r.URL.RawQuery
	}
	if len(path) > 511 {
		response(w, 400, "", nil)
		return
	}
	select {
	case s.reverseSlots <- struct{}{}:
		defer func() { <-s.reverseSlots }()
	default:
		response(w, 503, "", nil)
		return
	}
	digest := sha256.Sum256([]byte(path))
	prefix := fmt.Sprintf("reverse.%s.%X.", name, digest)
	if s.cacheRoot != nil {
		unlock, err := s.cacheRoot.LockContext(r.Context(), prefix)
		if err != nil {
			return
		}
		defer unlock()
		s.reverseMu.Lock()
		flush := target.flush && !s.reverseFlushed[name]
		s.reverseFlushed[name] = true
		s.reverseMu.Unlock()
		dir, err := s.cacheRoot.Open(".")
		if err == nil {
			defer dir.Close()
			for count := 0; count < 100000; {
				entries, readErr := dir.ReadDir(256)
				count += len(entries)
				for _, entry := range entries {
					if !strings.HasPrefix(entry.Name(), prefix) || entry.IsDir() {
						continue
					}
					stamp, parseErr := strconv.ParseInt(strings.TrimPrefix(entry.Name(), prefix), 10, 64)
					if parseErr != nil {
						continue
					}
					if flush || !time.Unix(stamp, 0).Add(target.ttl).After(s.Now()) {
						_ = s.cacheRoot.Remove(entry.Name())
						continue
					}
					file, openErr := s.cacheRoot.Open(entry.Name())
					if openErr != nil {
						continue
					}
					info, statErr := file.Stat()
					if statErr != nil || !info.Mode().IsRegular() || info.Size() > 64<<20 {
						file.Close()
						continue
					}
					w.Header().Set("Content-Type", target.mime)
					w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
					w.WriteHeader(200)
					_, _ = io.CopyN(w, file, info.Size())
					file.Close()
					return
				}
				if readErr != nil {
					break
				}
			}
		}
	}
	req, err := http.NewRequestWithContext(r.Context(), "GET", "https://"+target.host+path, nil)
	if err != nil {
		response(w, 400, "", nil)
		return
	}
	req.Header.Set("User-Agent", "teddyCloud/goteddycloud")
	res, err := s.MetadataHTTP.Do(req)
	if err != nil {
		response(w, 502, "", nil)
		return
	}
	defer res.Body.Close()
	// Buffer public lookup responses to validate completion before cache publication.
	// A bounded response also prevents malformed remote framing from poisoning cache.
	const limit = 64 << 20
	if res.ContentLength > limit {
		response(w, 502, "", nil)
		return
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil || len(data) > limit || res.ContentLength >= 0 && int64(len(data)) != res.ContentLength {
		response(w, 502, "", nil)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	if res.StatusCode == 200 && s.cacheRoot != nil {
		_ = s.cacheRoot.WriteAtomic(prefix+strconv.FormatInt(s.Now().Unix(), 10), data, 0600)
	}
	copyReverseHeaders(w.Header(), res.Header)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(res.StatusCode)
	_, _ = w.Write(data)
}

func copyReverseHeaders(destination, source http.Header) {
	blocked := map[string]bool{"Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true, "Proxy-Authorization": true, "Te": true, "Trailer": true, "Transfer-Encoding": true, "Upgrade": true, "Access-Control-Allow-Origin": true}
	for _, key := range strings.Split(source.Get("Connection"), ",") {
		blocked[http.CanonicalHeaderKey(strings.TrimSpace(key))] = true
	}
	for key, values := range source {
		if !blocked[http.CanonicalHeaderKey(key)] {
			destination[key] = append([]string(nil), values...)
		}
	}
	// Disable content sniffing when the reference upstream supplies no type.
	if destination.Get("Content-Type") == "" {
		destination["Content-Type"] = nil
	}
}
func relayReverse(w http.ResponseWriter, res *http.Response) {
	copyReverseHeaders(w.Header(), res.Header)
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, res.Body)
}
