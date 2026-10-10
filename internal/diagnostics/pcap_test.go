// SPDX-License-Identifier: GPL-2.0-only
package diagnostics

import (
	"bytes"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCapturePacketsChecksumsStreamingAndRestart(t *testing.T) {
	base := t.TempDir()
	f, err := Open(base, "traffic.pcap", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	capture, err := NewCapture(f)
	if err != nil {
		t.Fatal(err)
	}
	handler := capture.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(201)
		w.Write(data)
		w.(http.Flusher).Flush()
		w.Write(bytes.Repeat([]byte{'x'}, 70000))
	}))
	request := httptest.NewRequest("POST", "http://synthetic.invalid/upload", bytes.NewBufferString("synthetic-body"))
	request.RemoteAddr = "192.0.2.1:1234"
	request.Header.Set("Authorization", "synthetic-test-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != 201 || response.Body.Len() != 70014 {
		t.Fatal(response.Code, response.Body.Len())
	}
	data, err := os.ReadFile(filepath.Join(base, "traffic.pcap"))
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(data[20:24]) != 101 {
		t.Fatal("link type")
	}
	packets := 0
	var payload []byte
	for pos := 24; pos < len(data); {
		if pos+16 > len(data) {
			t.Fatal("partial record")
		}
		size := int(binary.LittleEndian.Uint32(data[pos+8:]))
		pos += 16
		if size < 40 || pos+size > len(data) {
			t.Fatal("packet size")
		}
		packet := data[pos : pos+size]
		pos += size
		packets++
		if checksum(packet[:20]) != 0 || int(binary.BigEndian.Uint16(packet[2:])) != size {
			t.Fatal("IP checksum/size")
		}
		pseudo := append([]byte{}, packet[12:20]...)
		pseudo = append(pseudo, 0, 6, byte((size-20)>>8), byte(size-20))
		pseudo = append(pseudo, packet[20:]...)
		if checksum(pseudo) != 0 {
			t.Fatal("TCP checksum")
		}
		payload = append(payload, packet[40:]...)
	}
	if packets < 6 || !bytes.Contains(payload, []byte("HTTP/1.1 201 Created")) || !bytes.Contains(payload, []byte("synthetic-body")) || !bytes.Contains(payload, []byte("synthetic-test-token")) {
		t.Fatal("missing traffic")
	}
	if _, err := NewCapture(f); err != nil {
		t.Fatal("restart", err)
	}
	info, _ := os.Stat(filepath.Join(base, "traffic.pcap"))
	if info.Mode().Perm() != 0600 {
		t.Fatal("permissions")
	}
}
func TestDiagnosticCapAndConfinement(t *testing.T) {
	base := t.TempDir()
	f, err := Open(base, "keys.log", 10)
	if err != nil {
		t.Fatal(err)
	}
	f.Write([]byte("12345678"))
	f.Write([]byte("abcdef"))
	f.Write([]byte("xy"))
	f.Close()
	data, _ := os.ReadFile(filepath.Join(base, "keys.log"))
	if string(data) != "12345678" {
		t.Fatal("cap", string(data))
	}
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(base, "escape"))
	for _, name := range []string{"../outside", filepath.Join(outside, "keys"), "escape/keys"} {
		if file, err := Open(base, name, 10); err == nil {
			file.Close()
			t.Fatal("escaped", name)
		}
	}
}
