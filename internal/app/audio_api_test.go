// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"goteddycloud/internal/taf"
)

func multipartAudio(t *testing.T, s *Server, path, name string, data []byte, ctx context.Context) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", path, &body).WithContext(ctx)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	result := httptest.NewRecorder()
	s.Management(result, request)
	return result
}
func TestAudioAPIsConvertPCMAndValidateUploadsWithoutDestroyingExistingFiles(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg unavailable")
	}
	s := fixtureApp(t)
	snap := s.Settings.Snapshot("")
	library := snap.String("internal.librarydirfull")
	for _, name := range []string{"tone1.wav", "tone2.wav"} {
		data, err := os.ReadFile(filepath.Join(snap.Base, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(library, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	form := url.Values{"source": {"tone1.wav", "tone2.wav"}, "target": {"converted.taf"}}.Encode()
	result := perform(s, "POST", "/api/fileEncode?special=library", []byte(form), false)
	if result.Code != 200 || result.Body.String() != "OK\r\n" {
		t.Fatal(result.Code, result.Body.String())
	}
	converted, err := os.ReadFile(filepath.Join(library, "converted.taf"))
	if err != nil {
		t.Fatal(err)
	}
	header, err := taf.Validate(bytes.NewReader(converted))
	if err != nil || len(header.TrackPageNums) != 2 {
		t.Fatal(header, err)
	}
	if result := perform(s, "POST", "/api/fileEncode?special=library", []byte(form), false); result.Code != 500 {
		t.Fatal("existing output overwritten", result.Code)
	}
	for _, form := range []string{"target=other.taf", "target=../escape&source=tone1.wav", "target=other.taf&source=missing", "target=other.taf&source=../escape"} {
		if result := perform(s, "POST", "/api/fileEncode?special=library", []byte(form), false); result.Code != 400 {
			t.Fatal(form, result.Code)
		}
	}
	result = multipartAudio(t, s, "/api/tafUpload?special=library&name=uploaded.taf", "uploaded.taf", converted, context.Background())
	if result.Code != 200 || result.Body.String() != "OK" {
		t.Fatal(result.Code, result.Body.String())
	}
	uploaded := filepath.Join(library, "uploaded.taf")
	before, err := os.ReadFile(uploaded)
	if err != nil || !bytes.Equal(before, converted) {
		t.Fatal("upload bytes changed", err)
	}
	result = multipartAudio(t, s, "/api/tafUpload?special=library&name=uploaded.taf", "uploaded.taf", []byte("corrupt"), context.Background())
	if result.Code != 500 || result.Body.String() != "TAF validation failed" {
		t.Fatal(result.Code, result.Body.String())
	}
	after, _ := os.ReadFile(uploaded)
	if !bytes.Equal(before, after) {
		t.Fatal("failed upload destroyed existing file")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	multipartAudio(t, s, "/api/tafUpload?special=library&name=uploaded.taf", "uploaded.taf", converted, ctx)
	after, _ = os.ReadFile(uploaded)
	if !bytes.Equal(before, after) {
		t.Fatal("canceled upload destroyed existing file")
	}
	result = multipartAudio(t, s, "/api/tafUpload?special=library&name=other.taf", "../escape.taf", converted, context.Background())
	if result.Code != 400 {
		t.Fatal("raw multipart path accepted", result.Code)
	}
	wav, err := os.ReadFile(filepath.Join(library, "tone1.wav"))
	if err != nil {
		t.Fatal(err)
	}
	result = multipartAudio(t, s, "/api/pcmUpload?special=library&name=pcm.taf&audioId=1234", "samples.pcm", wav[44:], context.Background())
	if result.Code != 200 || result.Body.String() != "OK" {
		t.Fatal(result.Code, result.Body.String())
	}
	file, err := os.Open(filepath.Join(library, "pcm.taf"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	header, err = taf.Validate(file)
	if err != nil || header.GetAudioId() != 1234 {
		t.Fatal(header, err)
	}
	result = multipartAudio(t, s, "/api/pcmUpload?special=library&name=pcm.taf&audioId=invalid", "samples.pcm", wav[44:], context.Background())
	if result.Code != 400 {
		t.Fatal("invalid ID accepted", result.Code)
	}
	if result := perform(s, "POST", "/api/tafUpload?special=library", []byte("not multipart"), false); result.Code != 500 {
		t.Fatal(result.Code)
	}
	outside := filepath.Join(t.TempDir(), "outside.wav")
	if err := os.WriteFile(outside, wav, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(library, "escape.wav")); err != nil {
		t.Skip(err)
	}
	result = perform(s, "POST", "/api/fileEncode?special=library", []byte("source=escape.wav&target=escape.taf"), false)
	if result.Code != 400 {
		t.Fatal("outside source read", result.Code)
	}
	if err := os.Symlink(outside, filepath.Join(library, "linked.taf")); err != nil {
		t.Skip(err)
	}
	result = multipartAudio(t, s, "/api/tafUpload?special=library&name=linked.taf", "linked.taf", converted, context.Background())
	// Replacing the symlink itself is allowed; the outside target must be intact.
	data, err := os.ReadFile(outside)
	if err != nil || !bytes.Equal(data, wav) {
		t.Fatal("outside file changed", err)
	}
	if result.Code != 200 {
		t.Fatal(result.Code, result.Body.String())
	}
}

func TestManagedPathsAndAudioAdmission(t *testing.T) {
	for _, path := range []string{"../x", "a/../x", "a\\x", "C:/x", "a\x00b"} {
		if _, err := managedPath("", path); err == nil {
			t.Fatal("unsafe path accepted", path)
		}
	}
	if path, err := managedPath("custom_img", "/folder/file"); err != nil || path != filepath.Join("custom_img", "folder", "file") {
		t.Fatal(path, err)
	}
	s := fixtureApp(t)
	for range cap(audioRequests) {
		audioRequests <- struct{}{}
	}
	defer func() {
		for range cap(audioRequests) {
			<-audioRequests
		}
	}()
	if result := perform(s, "POST", "/api/fileEncode", nil, false); result.Code != 503 {
		t.Fatal(result.Code)
	}
}
