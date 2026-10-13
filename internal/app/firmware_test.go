// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"goteddycloud/internal/certificates"
	"goteddycloud/internal/firmware"
)

func TestFirmwareWorkflowUsesCopiesAndPreflightsCertificateConflicts(t *testing.T) {
	s := fixtureApp(t)
	snap := s.Settings.Snapshot("")
	command := exec.Command("python3", "../../scripts/firmware-fixture.py", snap.Base)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatal(err, string(output))
	}
	source := filepath.Join(snap.String("internal.firmwaredirfull"), "firmware_001122334455.bin")
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	result := perform(s, "GET", "/api/esp32/patchFirmware?filename=firmware_001122334455.bin&hostname=tc.local&wifi_ssid=test-wifi&wifi_pass=test-secret", nil, false)
	if result.Code != 200 || result.Header().Get("Content-Type") != "binary/octet-stream" {
		t.Fatal(result.Code, result.Body.String())
	}
	if _, err := firmware.Parse(result.Body.Bytes()); err != nil {
		t.Fatal(err)
	}
	published := filepath.Join(snap.String("internal.firmwaredirfull"), "patched_001122334455.bin")
	before, _ := os.ReadFile(published)
	result = perform(s, "GET", "/api/esp32/patchFirmware?filename=firmware_001122334455.bin&hostname=hostname-longer-than-original-slot", nil, false)
	if result.Code != 400 {
		t.Fatal(result.Code)
	}
	after, _ := os.ReadFile(published)
	if !bytes.Equal(before, after) {
		t.Fatal("failed patch replaced previous output")
	}
	after, _ = os.ReadFile(source)
	if !bytes.Equal(original, after) {
		t.Fatal("source modified")
	}
	result = multipartAudio(t, s, "/api/esp32/uploadFirmware", "upload_001122334455", original, context.Background())
	if result.Code != 200 || result.Body.String() != "upload_001122334455.bin" {
		t.Fatal(result.Code, result.Body.String())
	}
	bad := append([]byte(nil), original...)
	bad[0xa000+32] ^= 1
	result = multipartAudio(t, s, "/api/esp32/uploadFirmware", "invalid", bad, context.Background())
	if result.Code != 400 {
		t.Fatal(result.Code)
	}
	if _, err := os.Stat(filepath.Join(snap.String("internal.firmwaredirfull"), "invalid.bin")); !os.IsNotExist(err) {
		t.Fatal("invalid upload published")
	}
	installed := filepath.Join(snap.String("internal.certdirfull"), "001122334455/ca.der")
	oldCA, _ := os.ReadFile(installed)
	otherCA, _ := os.ReadFile(filepath.Join(snap.String("internal.certdirfull"), "AABBCCDDEEFF/ca.der"))
	if err := os.WriteFile(installed, otherCA, 0600); err != nil {
		t.Fatal(err)
	}
	result = perform(s, "POST", "/api/esp32/extractCerts?filename=firmware_001122334455.bin", nil, false)
	if result.Code != 409 {
		t.Fatal(result.Code, result.Body.String())
	}
	after, _ = os.ReadFile(installed)
	if !bytes.Equal(after, otherCA) {
		t.Fatal("conflict changed installed CA")
	}
	result = perform(s, "POST", "/api/esp32/extractCerts?filename=firmware_001122334455.bin&overwrite=true", nil, false)
	if result.Code != 200 {
		t.Fatal(result.Code, result.Body.String())
	}
	after, _ = os.ReadFile(installed)
	if !bytes.Equal(after, oldCA) {
		t.Fatal("extracted CA differs")
	}
	cert, _ := os.ReadFile(filepath.Join(snap.String("internal.certdirfull"), "001122334455/client.der"))
	key, _ := os.ReadFile(filepath.Join(snap.String("internal.certdirfull"), "001122334455/private.der"))
	if err := certificates.ValidateIdentity(after, cert, key); err != nil {
		t.Fatal(err)
	}
	global := filepath.Join(snap.String("internal.certdirfull"), "ca.der")
	before, _ = os.ReadFile(global)
	result = multipartAudio(t, s, "/api/uploadCert", "ca.der", []byte("invalid"), context.Background())
	if result.Code != 400 {
		t.Fatal(result.Code)
	}
	after, _ = os.ReadFile(global)
	if !bytes.Equal(before, after) {
		t.Fatal("invalid material destroyed installed CA")
	}
	for _, endpoint := range []string{"/api/esp32/extractCerts?filename=../firmware_001122334455.bin", "/api/esp32/patchFirmware?filename=firmware_nothex00000.bin"} {
		result = perform(s, "POST", endpoint, nil, false)
		if result.Code < 400 {
			t.Fatal(endpoint, result.Code)
		}
	}
}

func TestDevelopCertificateUploadSelectsPrivateBoxDirectory(t *testing.T) {
	s := fixtureApp(t)
	id := "001122334455"
	path := filepath.Join(s.Settings.String("", "core.certdir"), "001122334455")
	if err := s.Settings.Set(id, "core.certdir", path); err != nil {
		t.Fatal(err)
	}
	snap := s.Settings.Snapshot(id)
	material, err := os.ReadFile(filepath.Join(s.Settings.Snapshot("").String("internal.certdirfull"), "001122334455/teddy-cert.pem"))
	if err != nil {
		t.Fatal(err)
	}
	result := multipartAudio(t, s, "/api/uploadCert?overlay="+id, "client.der", material, context.Background())
	if result.Code != 200 {
		t.Fatal(result.Code, result.Body.String())
	}
	path = filepath.Join(snap.String("internal.certdirfull"), "client.der")
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	if got := s.Settings.Snapshot(id).String("core.client_cert.file.crt"); got != filepath.Join(snap.String("core.certdir"), "client.der") {
		t.Fatal(got)
	}
}
