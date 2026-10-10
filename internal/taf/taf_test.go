// SPDX-License-Identifier: GPL-2.0-only
package taf

import (
	"bytes"
	"crypto/sha1"
	"io"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"

	"goteddycloud/internal/protocol"
)

func TestPaddingPreservesFrames(t *testing.T) {
	for _, packet := range [][]byte{{0xf8, 0xff, 0xfe}, {0xf9, 1, 2, 3, 4}, {0xfa, 1, 1, 2, 3}, {0xfb, 0x83, 1, 2, 1, 2, 3, 4, 5, 6}} {
		toc, original, err := OpusFrames(packet)
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range []int{64, 255, 256, 3543, 4053} {
			padded, err := PadOpus(packet, target)
			if err != nil {
				t.Fatal(err)
			}
			newTOC, frames, err := OpusFrames(padded)
			if err != nil {
				t.Fatal(err)
			}
			if newTOC&252 != toc&252 || len(frames) != len(original) || len(padded) != target {
				t.Fatal("padding changed packet shape")
			}
			for i := range frames {
				if !bytes.Equal(frames[i], original[i]) {
					t.Fatal("Opus frame bytes changed")
				}
			}
		}
	}
}
func TestHeaderAndPayloadValidation(t *testing.T) {
	payload := []byte("synthetic audio")
	sum := sha1.Sum(payload)
	h := &protocol.TonieboxAudioFileHeader{Sha1Hash: sum[:], NumBytes: proto.Uint64(uint64(len(payload))), AudioId: proto.Uint32(123), TrackPageNums: []uint32{0, 2}, XFill: []byte{}}
	header, err := MarshalHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	if len(header) != 4096 {
		t.Fatal(len(header))
	}
	file := append(header, payload...)
	parsed, err := Validate(bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.GetAudioId() != 123 {
		t.Fatal(parsed)
	}
	for _, bad := range [][]byte{file[:3], file[:100], file[:len(file)-1], append(append([]byte(nil), file...), 0)} {
		if _, err := Validate(bytes.NewReader(bad)); err == nil {
			t.Fatal("invalid file accepted")
		}
	}
}
func TestAlignedWriterCRCAndTracks(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "audio.taf"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	writer, err := NewWriter(f, 123)
	if err != nil {
		t.Fatal(err)
	}
	head := []byte{'O', 'p', 'u', 's', 'H', 'e', 'a', 'd', 1, 2, 0x38, 1, 0x80, 0xbb, 0, 0, 0, 0, 0}
	packet := []byte{0xfb, 3, 0xff, 0xfe, 0xff, 0xfe, 0xff, 0xfe} // three 20ms CBR frames
	for range 2 {
		if err := writer.StartTrack(head); err != nil {
			t.Fatal(err)
		}
		for range 3 {
			if err := writer.Packet(packet); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	_, _ = f.Seek(0, io.SeekStart)
	h, err := Validate(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.TrackPageNums) != 2 || h.TrackPageNums[0] != 0 || h.TrackPageNums[1] != 1 {
		t.Fatal(h.TrackPageNums)
	}
	if h.GetNumBytes()%4096 != 0 {
		t.Fatal("payload unaligned")
	}
	_, _ = f.Seek(4096, io.SeekStart)
	if err := CheckPages(f); err != nil {
		t.Fatal(err)
	}
	_, _ = f.Seek(4096, io.SeekStart)
	payload, _ := io.ReadAll(f)
	payload[len(payload)-1] ^= 1
	if err := CheckPages(bytes.NewReader(payload)); err == nil {
		t.Fatal("CRC damage accepted")
	}
}
func FuzzReadHeader(f *testing.F) {
	f.Add([]byte{0, 0, 0, 1, 0})
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = ReadHeader(bytes.NewReader(data)) })
}
func FuzzOpusFrames(f *testing.F) {
	f.Add([]byte{0xf8, 0xff, 0xfe})
	f.Fuzz(func(t *testing.T, data []byte) { _, _, _ = OpusFrames(data) })
}

// Exercise lacing-size transitions with real parser-valid variable-sized CBR
// packets. Every published page must still end exactly at a TAF block boundary.
func TestGroupedPagesAcrossLacingBoundaries(t *testing.T) {
	for length := 3; length < 1000; length += 7 {
		f, err := os.CreateTemp(t.TempDir(), "taf")
		if err != nil {
			t.Fatal(err)
		}
		w, err := NewWriter(f, 123)
		if err != nil {
			t.Fatal(err)
		}
		head := []byte{'O', 'p', 'u', 's', 'H', 'e', 'a', 'd', 1, 2, 0x38, 1, 0x80, 0xbb, 0, 0, 0, 0, 0}
		if err := w.StartTrack(head); err != nil {
			t.Fatal(err)
		}
		packet := append([]byte{0xfb, 3}, make([]byte, 3*length)...)
		for range 15 {
			if err := w.Packet(packet); err != nil {
				t.Fatalf("length %d: %v", length, err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatalf("length %d: %v", length, err)
		}
		_, _ = f.Seek(0, io.SeekStart)
		h, err := Validate(f)
		if err != nil {
			t.Fatal(err)
		}
		if h.GetNumBytes()%4096 != 0 {
			t.Fatal("unaligned")
		}
		_, _ = f.Seek(4096, io.SeekStart)
		if err := CheckPages(f); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}
