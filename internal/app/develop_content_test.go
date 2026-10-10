// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"goteddycloud/internal/content"
	"goteddycloud/internal/protocol"
	"goteddycloud/internal/taf"
)

func TestDevelopListenedMetadataAndSync(t *testing.T) {
	s := fixtureApp(t)
	snap := s.Settings.Snapshot("")
	root, _, err := s.Content.ManagedRoot(snap, "library")
	if err != nil {
		t.Fatal(err)
	}
	name := "listened.taf"
	uid := "0000000100000001"
	data := syntheticTAF(t)
	if err := root.WriteAtomic(name, data, 0600); err != nil {
		t.Fatal(err)
	}
	_ = root.WriteAtomic(name+".json", []byte(`{"unknown":{"keep":true}}`), 0600)
	w := perform(s, "POST", "/api/fileSetListened?special=library&path="+name, []byte("listened=true"), false)
	if w.Code != 200 || !fileMetadata(root, name).Bool("listened") {
		t.Fatal(w.Code, w.Body.String())
	}
	if !bytes.Contains(fileMetadata(root, name)["unknown"], []byte("true")) {
		t.Fatal("lost unknown metadata")
	}
	_ = perform(s, "POST", "/api/fileSetListened?special=library&path="+name, []byte("listened=false"), false)
	if err := s.Content.Update(snap, uid, func(m content.Metadata) error { m.Set("source", "lib://"+name); return nil }); err != nil {
		t.Fatal(err)
	}
	w = perform(s, "GET", "/v1/content/"+uid, nil, true)
	if w.Code != 200 || !fileMetadata(root, name).Bool("listened") {
		t.Fatal(w.Code, w.Body.String())
	}
	w = perform(s, "GET", "/api/fileIndexV2?special=library", nil, false)
	var index struct {
		Files []map[string]any `json:"files"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &index)
	found := false
	for _, row := range index.Files {
		if row["name"] == name+".json" {
			t.Fatal("sidecar visible", row)
		}
		if row["name"] == name {
			found = true
			if row["listened"] != true {
				t.Fatal(row)
			}
		}
	}
	if !found {
		t.Fatal(w.Body.String())
	}
}
func TestDevelopLengthSeconds(t *testing.T) {
	data := make([]byte, 4096*4)
	copy(data[4096+512:], "OggS")
	binary.LittleEndian.PutUint64(data[4096+512+6:], 48000)
	copy(data[4096*3:], "OggS")
	binary.LittleEndian.PutUint64(data[4096*3+6:], 48000*12)
	h := &protocol.TonieboxAudioFileHeader{TrackPageNums: []uint32{0}, NumBytes: protoUint64(4096 * 3)}
	if lengthSeconds(bytes.NewReader(data), h) != 11 {
		t.Fatal("duration")
	}
	binary.LittleEndian.PutUint64(data[4096*3+6:], 0)
	if lengthSeconds(bytes.NewReader(data), h) != 0 {
		t.Fatal("granule underflow")
	}
}
func protoUint64(v uint64) *uint64 { return &v }
func TestDevelopTrackExports(t *testing.T) {
	s := fixtureApp(t)
	snap := s.Settings.Snapshot("")
	data := syntheticTAF(t)
	path := filepath.Join(snap.String("internal.librarydirfull"), "export.taf")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"0", "100", "bad", "1,1", ""} {
		w := perform(s, "GET", "/content/export.taf?special=library&tracks="+q, nil, false)
		if w.Code != 400 {
			t.Fatal(q, w.Code)
		}
	}
	w := perform(s, "GET", "/content/export.taf?special=library&tracks=1", nil, false)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), data[4096:]) {
		t.Fatal(w.Code, len(w.Body.Bytes()), len(data))
	}
	h, err := taf.ReadHeader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	payload := append(append([]byte(nil), data[4096:]...), data[4096:]...)
	h.TrackPageNums = []uint32{0, 1}
	h.NumBytes = protoUint64(uint64(len(payload)))
	header, err := taf.MarshalHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	data = append(header, payload...)
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	w = perform(s, "GET", "/content/export.taf?special=library&tracks=1,2", nil, false)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	archive, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil || len(archive.File) != 2 {
		t.Fatal(err)
	}
	for i, entry := range archive.File {
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(reader)
		reader.Close()
		want := payload[:4096]
		if i == 1 {
			want = append(append([]byte(nil), payload[:512]...), payload[4096:]...)
		}
		if !bytes.Equal(got, want) {
			t.Fatal(entry.Name, "wrong track bytes")
		}
	}

}
