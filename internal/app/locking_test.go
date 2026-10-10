// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"goteddycloud/internal/content"
	"goteddycloud/internal/protocol"
	"goteddycloud/internal/storage"
	"goteddycloud/internal/taf"
)

type blockedResponse struct {
	header           http.Header
	entered, release chan struct{}
	once             sync.Once
}

func newBlockedResponse() *blockedResponse {
	return &blockedResponse{header: make(http.Header), entered: make(chan struct{}), release: make(chan struct{})}
}
func (w *blockedResponse) Header() http.Header { return w.header }
func (w *blockedResponse) WriteHeader(int)     {}
func (w *blockedResponse) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(p), nil
}
func awaitLockTest(t *testing.T, done <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal(message)
	}
}

func TestSlowImageResponseDoesNotBlockStatsOrFlush(t *testing.T) {
	s := fixtureApp(t)
	entry := &imageEntry{name: "slow.jpg", source: "https://example.invalid/slow.jpg", exists: true}
	if err := s.cacheRoot.WriteAtomic(entry.name, []byte("cached image"), 0600); err != nil {
		t.Fatal(err)
	}
	s.images[entry.name] = entry
	w := newBlockedResponse()
	served := make(chan struct{})
	go func() { defer close(served); s.cacheAPI(w, httptest.NewRequest("GET", "/cache/"+entry.name, nil)) }()
	release := sync.OnceFunc(func() { close(w.release) })
	defer func() { release(); <-served }()
	awaitLockTest(t, w.entered, "image did not start response")
	done := make(chan struct{})
	go func() { s.imageStats(); s.flushImages(context.Background(), []*imageEntry{entry}); close(done) }()
	awaitLockTest(t, done, "slow image response blocked cache management")
}

func TestImageFetchCancellationAndFlushPublication(t *testing.T) {
	s := fixtureApp(t)
	if err := s.Settings.Set("", "tonie_json.cache_images", "true"); err != nil {
		t.Fatal(err)
	}
	started, resume := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-resume; w.Write([]byte("image")) }))
	defer remote.Close()
	defer release()
	// Use the test server directly; this test concerns locking, not fetch policy.
	s.MetadataHTTP = remote.Client()
	name := strings.TrimPrefix(s.imageURL(context.Background(), remote.URL+"/image.jpg", false), "/cache/")
	entry := s.images[name]
	fetched := make(chan struct{})
	go func() { s.fetchImage(context.Background(), entry); close(fetched) }()
	defer func() { release(); <-fetched }()
	awaitLockTest(t, started, "fetch did not reach upstream")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cancelled := make(chan struct{})
	go func() { s.fetchImage(ctx, entry); s.flushImages(ctx, []*imageEntry{entry}); close(cancelled) }()
	awaitLockTest(t, cancelled, "cancelled cache operation waited for download")
	stats := make(chan struct{})
	go func() { s.imageStats(); close(stats) }()
	awaitLockTest(t, stats, "stats waited for download")
	// A live flush must wait until publication, then remove that publication.
	flushed := make(chan struct{})
	go func() { s.flushImages(context.Background(), []*imageEntry{entry}); close(flushed) }()
	select {
	case <-flushed:
		t.Fatal("flush bypassed active fetch")
	case <-time.After(20 * time.Millisecond):
	}
	release()
	awaitLockTest(t, fetched, "fetch did not finish")
	awaitLockTest(t, flushed, "flush did not finish")
	if _, err := s.cacheRoot.Stat(name); !os.IsNotExist(err) {
		t.Fatal("fetch undid completed flush", err)
	}
}

func TestFirmwareResponseDoesNotHoldCertificateLocks(t *testing.T) {
	s := fixtureApp(t)
	path := s.Settings.String("", "internal.certdirfull")
	data, err := os.ReadFile(filepath.Join(path, "001122334455", "ca.der"))
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "ca.der")
	if err != nil {
		t.Fatal(err)
	}
	part.Write(data)
	form.Close()
	request := httptest.NewRequest("POST", "/api/uploadCert", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	w := newBlockedResponse()
	served := make(chan struct{})
	go func() { defer close(served); s.uploadCertificates(w, request) }()
	release := sync.OnceFunc(func() { close(w.release) })
	defer func() { release(); <-served }()
	awaitLockTest(t, w.entered, "certificate response did not start")
	root, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	unlock, err := root.LockContext(ctx, "ca.der")
	if err != nil {
		t.Fatal("response retained certificate transaction lock", err)
	}
	unlock()
}

func TestAssignmentReplacementSurvivesPreviousConsumption(t *testing.T) {
	s := fixtureApp(t)
	path := "/api/assignUnknown?special=library&path=same.taf"
	if r := perform(s, "POST", path, nil, false); r.Code != 200 {
		t.Fatal(r.Code)
	}
	source, version := s.pendingAssignment()
	// Even replacing with the identical path creates a new reservation.
	if r := perform(s, "POST", path, nil, false); r.Code != 200 {
		t.Fatal(r.Code)
	}
	s.consumeAssignment(source, version)
	if s.Settings.String("", "internal.assign_unknown") != source {
		t.Fatal("old attempt consumed new assignment")
	}
	source, version = s.pendingAssignment()
	s.consumeAssignment(source, version)
	if s.Settings.String("", "internal.assign_unknown") != "" {
		t.Fatal("current reservation was not consumed")
	}
}

func TestFreshnessDoesNotWaitForRTNLLogging(t *testing.T) {
	s := fixtureApp(t)
	s.rtnlMu.Lock()
	done := make(chan struct{})
	go func() {
		s.localFreshness("", s.Settings.Snapshot(""), new(protocol.TonieFreshnessCheckRequest))
		close(done)
	}()
	defer func() { s.rtnlMu.Unlock(); <-done }()
	awaitLockTest(t, done, "logging blocked freshness")
}

func TestPlaylistDestinationCoordinatesBoxesAndReleasesBeforeResponse(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip(err)
	}
	s := fixtureApp(t)
	if err := s.Settings.Set("", "encode.ffmpeg_stream_buffer_ms", "0"); err != nil {
		t.Fatal(err)
	}
	snap := s.Settings.Snapshot("")
	root, _, err := s.Content.ManagedRoot(snap, "library")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(snap.Base, "tone1.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if err := root.WriteAtomic("tone.wav", data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteAtomic("shared.tap", []byte(`{"type":"tap","audio_id":2468,"filepath":"lib://shared.taf","files":[{"filepath":"lib://tone.wav"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	const uid = "8877665544332211"
	if err := s.Content.Update(snap, uid, func(m content.Metadata) error {
		m.Set("source", "lib://shared.tap")
		m.Set("nocloud", true)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	metadata, err := s.Content.Metadata(snap, uid)
	if err != nil {
		t.Fatal(err)
	}
	held := root.Lock("shared.taf")
	for _, id := range []string{"boxA", "boxB"} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		request := httptest.NewRequest("GET", "/v2/content/"+uid, nil).WithContext(ctx)
		reply := httptest.NewRecorder()
		s.serveLive(reply, request, id, snap, uid, metadata)
		cancel()
		if reply.Code != 503 {
			held()
			t.Fatal("box bypassed shared destination", id, reply.Code)
		}
	}
	held()
	w := newBlockedResponse()
	served := make(chan struct{})
	go func() {
		defer close(served)
		s.serveStored(w, httptest.NewRequest("GET", "/v2/content/"+uid, nil), "boxA", snap, uid)
	}()
	release := sync.OnceFunc(func() { close(w.release) })
	defer func() { release(); <-served }()
	awaitLockTest(t, w.entered, "playlist response did not start")
	// Generation/publication must finish despite the blocked response.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	unlock, err := root.LockContext(ctx, "shared.taf")
	if err != nil {
		t.Fatal("response held generation lock", err)
	}
	file, err := root.Open("shared.taf")
	unlock()
	if err != nil {
		t.Fatal(err)
	}
	header, err := taf.Validate(file)
	before, _ := file.Stat()
	file.Close()
	if err != nil || header.GetAudioId() != 2468 {
		t.Fatal("invalid publication", err)
	}
	second := httptest.NewRecorder()
	s.serveStored(second, httptest.NewRequest("GET", "/v2/content/"+uid, nil), "boxB", snap, uid)
	if second.Code != 200 {
		t.Fatal(second.Code)
	}
	if _, err := taf.Validate(bytes.NewReader(second.Body.Bytes())); err != nil {
		t.Fatal("second box missed completed output", err)
	}
	after, err := root.Stat("shared.taf")
	if err != nil || !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("second box regenerated output", err)
	}
}

func TestImageFetchCoalescesConcurrentMisses(t *testing.T) {
	s := fixtureApp(t)
	if err := s.Settings.Set("", "tonie_json.cache_images", "true"); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	started, resume := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(resume) })
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-resume
		w.Write([]byte("image"))
	}))
	defer remote.Close()
	defer release()
	s.MetadataHTTP = remote.Client()
	name := strings.TrimPrefix(s.imageURL(context.Background(), remote.URL+"/shared.jpg", false), "/cache/")
	entry := s.images[name]
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() { defer workers.Done(); s.fetchImage(context.Background(), entry) }()
	}
	defer func() { release(); workers.Wait() }()
	awaitLockTest(t, started, "fetch did not start")
	release()
	workers.Wait()
	if calls.Load() != 1 {
		t.Fatal("concurrent cache misses duplicated fetch", calls.Load())
	}
}
