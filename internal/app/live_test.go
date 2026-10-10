// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"goteddycloud/internal/content"
	"goteddycloud/internal/protocol"
	"goteddycloud/internal/taf"
)

func radioHeader() []byte {
	data := make([]byte, 44)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], 0x7fffff00)
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 2)
	binary.LittleEndian.PutUint32(data[24:], 48000)
	binary.LittleEndian.PutUint32(data[28:], 192000)
	binary.LittleEndian.PutUint16(data[32:], 4)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], 0x7ffffedc)
	return data
}
func TestLiveHTTPSourceStopIsolatesBoxesAndRetainsValidatedPrefix(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg unavailable")
	}
	s := fixtureApp(t)
	s.Settings.Set("", "encode.ffmpeg_stream_buffer_ms", "0")
	radio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(radioHeader())
		w.(http.Flusher).Flush()
		silence := make([]byte, 16000)
		for {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(10 * time.Millisecond):
				if _, err := w.Write(silence); err != nil {
					return
				}
				w.(http.Flusher).Flush()
			}
		}
	}))
	defer radio.Close()
	const ruid = "1122334455667788"
	for _, id := range []string{"001122334455", "AABBCCDDEEFF"} {
		if err := s.Content.Update(s.Settings.Snapshot(id), ruid, func(m content.Metadata) error {
			m.Set("source", radio.URL+"/radio.wav")
			m.Set("nocloud", true)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	completed := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/")
		s.serveStored(w, r, id, s.Settings.Snapshot(id), ruid)
		completed <- id
	}))
	defer server.Close()
	client := &http.Client{Timeout: 10 * time.Second}
	first, err := client.Get(server.URL + "/001122334455")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Body.Close()
	second, err := client.Get(server.URL + "/AABBCCDDEEFF")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Body.Close()
	for _, res := range []*http.Response{first, second} {
		if res.StatusCode != 200 {
			t.Fatal(res.StatusCode)
		}
		data := make([]byte, 4096)
		if _, err := io.ReadFull(res.Body, data); err != nil {
			t.Fatal(err)
		}
		if _, err := taf.ReadHeader(strings.NewReader(string(data))); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Commands.Execute("001122334455", "CmdStop", ""); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-completed:
		if id != "001122334455" {
			t.Fatal("wrong box stopped", id)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("stopped handler did not exit")
	}
	select {
	case id := <-completed:
		t.Fatal("second box stopped", id)
	default:
	}
	if err := s.Commands.Execute("AABBCCDDEEFF", "CmdStop", ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-completed:
	case <-time.After(4 * time.Second):
		t.Fatal("second handler did not exit")
	}
	// Validate the retained prefix from the shared fixture content root.
	root, _, err := s.Content.ManagedRoot(s.Settings.Snapshot("001122334455"), "")
	if err != nil {
		t.Fatal(err)
	}
	path, _ := content.UIDPath(ruid)
	file, err := root.Open(path + ".stream.001122334455")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := taf.Validate(file); err != nil {
		t.Fatal(err)
	}
	if reply := s.localFreshness("001122334455", s.Settings.Snapshot("001122334455"), &protocol.TonieFreshnessCheckRequest{TonieInfos: []*protocol.TonieFCInfo{{Uid: proto.Uint64(func() uint64 { uid, _ := content.UIDFromRUID(ruid); return uid }()), AudioId: proto.Uint32(0)}}}); len(reply.TonieMarked) != 1 {
		t.Fatal("radio not marked fresh")
	}
}

func TestUncachedTAPStreamsThenPublishesValidatedCache(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg unavailable")
	}
	s := fixtureApp(t)
	s.Settings.Set("", "encode.ffmpeg_stream_buffer_ms", "0")
	snap := s.Settings.Snapshot("")
	root, _, err := s.Content.ManagedRoot(snap, "library")
	if err != nil {
		t.Fatal(err)
	}
	// Fixture tones live under the data root; copy them through explicit trusted test paths.
	for _, name := range []string{"tone1.wav", "tone2.wav"} {
		data, err := os.ReadFile(filepath.Join(snap.Base, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := root.WriteAtomic(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := root.WriteAtomic("live.tap", []byte(`{"type":"tap","audio_id":2468,"filepath":"lib://live-cache.taf","files":[{"filepath":"lib://tone1.wav"},{"filepath":"lib://tone2.wav"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	const ruid = "8877665544332211"
	if err := s.Content.Update(snap, ruid, func(m content.Metadata) error { m.Set("source", "lib://live.tap"); m.Set("nocloud", true); return nil }); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/v2/content/"+ruid, nil)
	response := httptest.NewRecorder()
	s.serveStored(response, request, "", snap, ruid)
	if response.Code != 200 || response.Body.Len() < 8192 {
		t.Fatal(response.Code, response.Body.Len())
	}
	file, err := root.Open("live-cache.taf")
	if err != nil {
		t.Fatal(err)
	}
	header, err := taf.Validate(file)
	file.Close()
	if err != nil || header.GetAudioId() != 2468 || len(header.TrackPageNums) != 2 {
		t.Fatal(header, err)
	}
	next := httptest.NewRecorder()
	s.serveStored(next, request, "", snap, ruid)
	if _, err := taf.Validate(bytes.NewReader(next.Body.Bytes())); err != nil {
		t.Fatal("second request did not use validated cache", err)
	}
}

func TestGrowingUIDFileDeliversAppendedBlocks(t *testing.T) {
	s := fixtureApp(t)
	snap := s.Settings.Snapshot("")
	root, _, err := s.Content.ManagedRoot(snap, "")
	if err != nil {
		t.Fatal(err)
	}
	temporary, err := os.CreateTemp(t.TempDir(), "complete-*.taf")
	if err != nil {
		t.Fatal(err)
	}
	defer temporary.Close()
	writer, err := taf.NewWriter(temporary, 1234)
	if err != nil {
		t.Fatal(err)
	}
	head := []byte{'O', 'p', 'u', 's', 'H', 'e', 'a', 'd', 1, 2, 0x38, 1, 0x80, 0xbb, 0, 0, 0, 0, 0}
	for range 2 {
		writer.StartTrack(head)
		if err := writer.Packet([]byte{0xfb, 3, 0xff, 0xfe, 0xff, 0xfe, 0xff, 0xfe}); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(temporary.Name())
	if err != nil {
		t.Fatal(err)
	}
	const ruid = "1122334488776655"
	path, _ := content.UIDPath(ruid)
	root.MkdirAll(filepath.Dir(path), 0700)
	if err := root.WriteAtomic(path, data[:8192], 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.serveStored(w, r, "", snap, ruid) }))
	defer server.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	first := make([]byte, 8192)
	if _, err := io.ReadFull(response.Body, first); err != nil {
		t.Fatal(err)
	}
	appendFile, err := root.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	appendFile.Write(data[8192:])
	appendFile.Close()
	remaining, err := io.ReadAll(response.Body)
	if err != io.ErrUnexpectedEOF {
		t.Fatal("expected finite prefix of advertised stream", err)
	}
	received := append(first, remaining...)
	if !bytes.Equal(received, data) {
		t.Fatal("growing blocks changed or missing")
	}
	if _, err := taf.Validate(bytes.NewReader(received)); err != nil {
		t.Fatal(err)
	}
}

func TestDevelopLiveRestartUsesExistingPrefixWithoutEncoder(t *testing.T) {
	s := fixtureApp(t)
	snap := s.Settings.Snapshot("")
	uid := "1122334455667788"
	if err := s.Content.Update(snap, uid, func(m content.Metadata) error {
		m.Set("source", "http://127.0.0.1:1/unreachable")
		m.Set("nocloud", true)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	root, _, err := s.Content.ManagedRoot(snap, "")
	if err != nil {
		t.Fatal(err)
	}
	name, _ := content.UIDPath(uid)
	prefix := syntheticTAF(t)
	if err := root.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteAtomic(name+".stream", prefix, 0600); err != nil {
		t.Fatal(err)
	}
	_ = s.Settings.Set("", "encode.ffmpeg_stream_restart", "true")
	r := httptest.NewRequest("GET", "/v1/content/"+uid, nil)
	r.Header.Set("Range", "bytes=1-100")
	w := httptest.NewRecorder()
	s.Device(w, r)
	if w.Code != 206 || !bytes.Equal(w.Body.Bytes(), prefix[1:101]) {
		t.Fatal(w.Code, w.Body.String())
	}
}
