// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"context"
	"encoding/json"
	"goteddycloud/internal/content"
	"goteddycloud/internal/taf"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"goteddycloud/internal/certificates"
	"goteddycloud/internal/config"
	"goteddycloud/internal/diagnostics"
	"goteddycloud/internal/firmware"
	"goteddycloud/internal/fixtures"
	"goteddycloud/internal/storage"
	"goteddycloud/internal/teststorage"
)

func remoteFixture(t *testing.T) (string, storage.Backend) {
	t.Helper()
	source := filepath.Join(t.TempDir(), "source")
	if err := fixtures.Generate(source); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(t.TempDir(), "remote-deployment") // Deliberately never created locally.
	backend := teststorage.New(t, base, nil)
	root, err := backend.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	err = filepath.WalkDir(source, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, name)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return root.MkdirAll(relative, 0700)
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		return root.WriteAtomic(relative, data, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	library, err := backend.OpenRoot(filepath.Join(base, "data/library"))
	if err != nil {
		t.Fatal(err)
	}
	if err = library.WriteAtomic("synthetic.taf", syntheticTAF(t), 0600); err != nil {
		t.Fatal(err)
	}
	library.Close()
	return base, backend
}

func TestS3DeploymentPersistence(t *testing.T) {
	base, backend := remoteFixture(t)
	settings, err := config.OpenWithStorage(base, backend, false)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(settings)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close(); settings.Close() }()
	check := func(method, path, body string) {
		t.Helper()
		w := perform(s, method, path, []byte(body), false)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/web/", "/api/getBoxes", "/api/getTagInfo?ruid=0000000100000001", "/api/settings/getIndex", "/api/toniesJson", "/api/fileIndex?special=library"} {
		check("GET", path, "")
	}
	content := perform(s, "GET", "/v1/content/0000000100000001", nil, true)
	if content.Code != 200 || content.Body.Len() < 4096 {
		t.Fatalf("content: %d %s", content.Code, content.Body.String())
	}
	check("POST", "/api/settings/set/toniebox.max_vol_spk?overlay=001122334455", "2")
	check("POST", "/api/toniesCustomJsonUpsert", `{"model":"SYNTHETIC_001","series":"Remote"}`)
	check("POST", "/api/toniesCustomJsonRename", `{"fromModel":"SYNTHETIC_001","toModel":"REMOTE_001"}`)
	metadata, err := s.Content.Metadata(settings.Snapshot(""), "0000000100000001")
	if err != nil || metadata.String("tonie_model") != "REMOTE_001" {
		t.Fatal("linked remote rename", metadata, err)
	}
	check("POST", "/api/auth/users/create", `{"username":"storage-admin","password":"synthetic-password"}`)
	check("POST", "/api/dirCreate?special=library", "folder")
	root, err := backend.OpenRoot(settings.String("", "internal.librarydirfull"))
	if err != nil {
		t.Fatal(err)
	}
	if err = root.WriteAtomic("folder/name # ä.txt", []byte("remote artifact"), 0600); err != nil {
		t.Fatal(err)
	}
	check("POST", "/api/fileMove?special=library", "source=folder&target=moved")
	if data, err := root.ReadFile("moved/name # ä.txt"); err != nil || string(data) != "remote artifact" {
		t.Fatal("directory move", string(data), err)
	}
	root.Close()
	snap := settings.Snapshot("")
	for _, prefix := range []string{"core.server_cert", "core.server_cert_tb2"} {
		if _, err = certificates.Load(snap, prefix); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range []string{"rtnl.logRaw", "rtnl.logHuman"} {
		if err = settings.Set("", key, "true"); err != nil {
			t.Fatal(err)
		}
	}
	s.logRTNL(settings.Snapshot(""), []byte{1, 2, 3})
	if data, err := backend.ReadFile(filepath.Join(base, "config/rtnl.bin")); err != nil || len(data) != 7 {
		t.Fatal("RTNL persistence", data, err)
	}
	diagnostic, err := diagnostics.OpenWithStorage(base, "keys.log", 4096, backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = diagnostic.Write([]byte("synthetic diagnostic\n")); err != nil {
		t.Fatal(err)
	}
	data, err := backend.ReadFile(filepath.Join(base, "keys.log"))
	if err != nil || !bytes.Contains(data, []byte("synthetic")) {
		t.Fatal("diagnostic persistence before close", err)
	}
	if err = diagnostic.Close(); err != nil {
		t.Fatal(err)
	}
	s.Close()
	settings.Close()
	settings, err = config.OpenWithStorage(base, backend, false)
	if err != nil {
		t.Fatal(err)
	}
	s, err = New(settings)
	if err != nil {
		t.Fatal(err)
	}
	if settings.String("001122334455", "toniebox.max_vol_spk") != "2" {
		t.Fatal("overlay did not survive restart")
	}
	if len(s.auth.users) != 1 || s.auth.users[0].Username != "storage-admin" {
		t.Fatal("users did not survive restart")
	}
	if _, err = os.Stat(base); !os.IsNotExist(err) {
		t.Fatalf("S3 deployment created local durable files: %v", err)
	}
}

func TestS3OTACacheAndContentMigration(t *testing.T) {
	base, backend := remoteFixture(t)
	settings, err := config.OpenWithStorage(base, backend, false)
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	s, err := New(settings)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, key := range []string{"cloud.enabled", "cloud.enableV1Ota", "cloud.cacheOta", "cloud.localOta"} {
		if err = settings.Set("", key, "true"); err != nil {
			t.Fatal(err)
		}
	}
	cloud := &otaCloud{data: []byte("synthetic firmware"), length: 18, filename: "300-test.bin"}
	s.Cloud = cloud
	first := perform(s, "GET", "/v1/ota/2?cv=0", nil, true)
	if first.Code != 304 {
		t.Fatalf("OTA: %d %s", first.Code, first.Body.String())
	}
	second := perform(s, "GET", "/v1/ota/2?cv=0", nil, true)
	if second.Code != 200 || second.Body.String() != "synthetic firmware" || cloud.calls != 2 {
		t.Fatal("OTA not cached", cloud.calls, second.Code)
	}
	const uid = "0200000002000000"
	cache, err := s.Content.BeginCache(settings.Snapshot(""), uid)
	if err != nil {
		t.Fatal(err)
	}
	data := syntheticTAF(t)
	if _, err = cache.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = cache.Commit(context.Background(), int64(len(data))); err != nil {
		t.Fatal(err)
	}
	if err = cache.Close(); err != nil {
		t.Fatal(err)
	}
	if err = s.Content.MigrateToLibrary(context.Background(), settings.Snapshot(""), uid, true); err != nil {
		t.Fatal(err)
	}
	result := perform(s, "GET", "/v1/content/"+uid, nil, true)
	if result.Code != 200 || result.Body.Len() < 4096 {
		t.Fatal("migrated content unavailable", result.Code)
	}
	metadata, err := s.Content.Metadata(settings.Snapshot(""), uid)
	if err != nil || !strings.HasPrefix(metadata.String("source"), "lib://") {
		t.Fatal(metadata, err)
	}
}

func TestS3PlaylistLiveImagesAndGeneratedCertificates(t *testing.T) {
	base, backend := remoteFixture(t)
	settings, err := config.OpenWithStorage(base, backend, false)
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	s, err := New(settings)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	snap := settings.Snapshot("")
	// Generate fresh material and a client identity entirely in remote storage.
	for _, key := range []string{"crt", "key", "ca", "ca_key", "ca_der"} {
		snap.Values["core.server_cert.file."+key] = "certs/generated/" + key + ".pem"
	}
	snap.Values["internal.autogen_certs"] = "true"
	if _, err = certificates.Ensure(snap, "core.server_cert"); err != nil {
		t.Fatal(err)
	}
	if err = backend.MkdirAll(filepath.Join(base, "certs/generated-client"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err = certificates.GenerateClient(snap, "ABCDEF123456", filepath.Join(base, "certs/generated-client"), false); err != nil {
		t.Fatal(err)
	}
	if err = settings.Set("", "core.allowPrivateFetch", "true"); err != nil {
		t.Fatal(err)
	}
	if err = settings.Set("", "tonie_json.cache_images", "true"); err != nil {
		t.Fatal(err)
	}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("synthetic image")) }))
	defer remote.Close()
	imagePath := s.imageURL(context.Background(), remote.URL+"/image.png", false)
	for range 2 {
		w := perform(s, "GET", imagePath, nil, false)
		if w.Code != 200 || w.Body.String() != "synthetic image" {
			t.Fatal("image cache", w.Code, w.Body.String())
		}
	}
	if w := perform(s, "POST", "/api/cacheFlush", nil, false); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err = exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg unavailable; certificates and images passed")
	}
	library, err := backend.OpenRoot(snap.String("internal.librarydirfull"))
	if err != nil {
		t.Fatal(err)
	}
	defer library.Close()
	for _, name := range []string{"tone1.wav", "tone2.wav"} {
		data, err := backend.ReadFile(filepath.Join(base, name))
		if err != nil {
			t.Fatal(err)
		}
		if err = library.WriteAtomic(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	playlist := content.Playlist{Type: "tap", AudioID: 123, Filepath: "lib://generated.taf", Name: "Remote", Files: []content.PlaylistFile{{Filepath: "lib://tone1.wav"}, {Filepath: "lib://tone2.wav"}}}
	data, _ := json.Marshal(playlist)
	if err = library.WriteAtomic("playlist.tap", append(bytes.Repeat([]byte(" "), 512), data...), 0600); err != nil {
		t.Fatal(err)
	}
	const uid = "0000000100000001"
	if err = s.Content.Update(snap, uid, func(m content.Metadata) error { m.Set("source", "lib://playlist.tap"); return nil }); err != nil {
		t.Fatal(err)
	}
	w := perform(s, "GET", "/v1/content/"+uid, nil, true)
	if w.Code != 200 {
		t.Fatal("playlist", w.Code, w.Body.String())
	}
	header, err := taf.Validate(bytes.NewReader(w.Body.Bytes()))
	if err != nil || header.GetAudioId() != 123 {
		t.Fatal("playlist TAF", header, err)
	}
	if err = settings.Set("", "encode.ffmpeg_stream_buffer_ms", "0"); err != nil {
		t.Fatal(err)
	}
	if err = s.Content.Update(snap, uid, func(m content.Metadata) error { m.Set("source", "lib://tone1.wav"); m.Set("live", true); return nil }); err != nil {
		t.Fatal(err)
	}
	w = perform(s, "GET", "/v1/content/"+uid, nil, true)
	if w.Code != 200 || w.Body.Len() < 4096 {
		t.Fatal("live", w.Code, w.Body.Len())
	}
	root, err := backend.OpenRoot(snap.String("internal.contentdirfull"))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	output, err := root.Open("00000001/00000001.stream")
	if err != nil {
		t.Fatal("live publication", err)
	}
	defer output.Close()
	if _, err = taf.Validate(output); err != nil {
		t.Fatal("published live TAF", err)
	}
	if _, err = os.Stat(base); !os.IsNotExist(err) {
		t.Fatal("remote deployment written locally", err)
	}
}

func TestS3FirmwareWorkflow(t *testing.T) {
	base, backend := remoteFixture(t)
	settings, err := config.OpenWithStorage(base, backend, false)
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	s, err := New(settings)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	source := filepath.Join(t.TempDir(), "synthetic-firmware-source")
	if err = fixtures.Generate(source); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("python3", "../../scripts/firmware-fixture.py", source)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	original, err := os.ReadFile(filepath.Join(source, "data/firmware/firmware_001122334455.bin"))
	if err != nil {
		t.Fatal(err)
	}
	uploaded := multipartAudio(t, s, "/api/esp32/uploadFirmware", "firmware_001122334455", original, context.Background())
	if uploaded.Code != 200 {
		t.Fatal("firmware upload", uploaded.Code, uploaded.Body.String())
	}
	patched := perform(s, "GET", "/api/esp32/patchFirmware?filename=firmware_001122334455.bin&hostname=tc.local", nil, false)
	if patched.Code != 200 {
		t.Fatal("firmware patch", patched.Code, patched.Body.String())
	}
	if _, err = firmware.Parse(patched.Body.Bytes()); err != nil {
		t.Fatal(err)
	}
	root, err := backend.OpenRoot(settings.String("", "internal.firmwaredirfull"))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	stored, err := root.ReadFile("patched_001122334455.bin")
	if err != nil || !bytes.Equal(stored, patched.Body.Bytes()) {
		t.Fatal("patched publication", err)
	}
	stored, err = root.ReadFile("firmware_001122334455.bin")
	if err != nil || !bytes.Equal(stored, original) {
		t.Fatal("original firmware changed", err)
	}
	extracted := perform(s, "POST", "/api/esp32/extractCerts?filename=firmware_001122334455.bin&overwrite=true", nil, false)
	if extracted.Code != 200 {
		t.Fatal("certificate extraction", extracted.Code, extracted.Body.String())
	}
	if _, err = os.Stat(base); !os.IsNotExist(err) {
		t.Fatal("remote firmware written locally", err)
	}
}
