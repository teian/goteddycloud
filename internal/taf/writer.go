// SPDX-License-Identifier: GPL-2.0-only
package taf

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"

	"google.golang.org/protobuf/proto"

	"goteddycloud/internal/protocol"
)

// Writer groups complete 60ms Opus packets into block-aligned Ogg pages.
// Only the final packet of each page is padded; chapter starts flush the page.
type Writer struct {
	out     io.WriteSeeker
	hash    hash.Hash
	id      uint32
	seq     uint32
	bytes   uint64
	granule uint64
	packets uint64
	tracks  []uint32
	headers bool
	closed  bool
	pending [][]byte
}

func NewWriter(out io.WriteSeeker, id uint32) (*Writer, error) {
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if _, err := out.Write(make([]byte, BlockSize)); err != nil {
		return nil, err
	}
	return &Writer{out: out, hash: sha1.New(), id: id}, nil
}
func (w *Writer) page(packets [][]byte, granule uint64, flags byte) error {
	data, err := Page(packets, w.id, w.seq, granule, flags)
	if err != nil {
		return err
	}
	if _, err := w.out.Write(data); err != nil {
		return err
	}
	_, _ = w.hash.Write(data)
	w.seq++
	w.bytes += uint64(len(data))
	return nil
}
func (w *Writer) StartTrack(head []byte) error {
	if w.closed {
		return errors.New("TAF writer closed")
	}
	if len(w.tracks) >= 99 {
		return errors.New("TAF chapter limit")
	}
	if len(head) != 19 || !bytes.Equal(head[:8], []byte("OpusHead")) || head[9] != 2 || binary.LittleEndian.Uint32(head[12:]) != 48000 {
		return errors.New("expected stereo 48kHz OpusHead")
	}
	if !w.headers {
		if err := w.page([][]byte{head}, 0, 2); err != nil {
			return err
		}
		tags := make([]byte, 436)
		copy(tags, "OpusTags")
		vendor := []byte("goTeddyCloud")
		binary.LittleEndian.PutUint32(tags[8:], uint32(len(vendor)))
		copy(tags[12:], vendor)
		// Zero comments, remaining bytes are permissible comment padding.
		if err := w.page([][]byte{tags}, 0, 0); err != nil {
			return err
		}
		if w.bytes != 512 {
			return fmt.Errorf("initial Ogg header bytes %d", w.bytes)
		}
		w.headers = true
		w.packets = 2
	}
	if err := w.flush(); err != nil {
		return err
	}
	w.tracks = append(w.tracks, uint32(w.bytes/BlockSize))
	return nil
}
func (w *Writer) Packet(packet []byte) error {
	if !w.headers || w.closed {
		return errors.New("TAF writer not ready")
	}
	toc, frames, err := OpusFrames(packet)
	if err != nil {
		return err
	}
	samples := OpusSamples(toc) * len(frames)
	if samples != 2880 {
		return fmt.Errorf("expected 60ms Opus packet, got %d samples", samples)
	}
	space := BlockSize - int(w.bytes%BlockSize)
	candidate := append(append([][]byte(nil), w.pending...), packet)
	page, err := Page(candidate, w.id, w.seq, w.granule+uint64(samples), 0)
	segments := 0
	for _, p := range candidate {
		segments += len(p)/255 + 1
	}
	// Leave lacing room for block padding when very small Opus packets
	// (notably silence) would otherwise exhaust the 255-entry table.
	if err != nil || len(page) > space-4 || segments > 237 {
		if len(w.pending) == 0 {
			return errors.New("invalid Opus packet exceeds block capacity")
		}
		if err := w.flush(); err != nil {
			return err
		}
		candidate = [][]byte{packet}
		page, err = Page(candidate, w.id, w.seq, w.granule+uint64(samples), 0)
		if err != nil || len(page) > BlockSize {
			return errors.New("invalid Opus packet exceeds block capacity")
		}
	}
	w.pending = candidate
	w.granule += uint64(samples)
	w.packets++
	return nil
}
func (w *Writer) flush() error {
	if len(w.pending) == 0 {
		return nil
	}
	space := BlockSize - int(w.bytes%BlockSize)
	packets := append([][]byte(nil), w.pending...)
	last := len(packets) - 1
	var padded []byte
	// Ogg lacing adds an extra segment at multiples of 255. If padding only
	// the final packet hits that one-byte hole, adjust the preceding packet.
	for adjustment := 0; adjustment <= 255; adjustment++ {
		if adjustment > 0 {
			if last == 0 {
				break
			}
			p, err := PadOpus(w.pending[0], len(w.pending[0])+adjustment)
			if err != nil {
				continue
			}
			packets[0] = p
		}
		fixed := 27
		for _, packet := range packets[:last] {
			fixed += len(packet) + len(packet)/255 + 1
		}
		target := -1
		for n := space - fixed - 1; n >= len(packets[last]); n-- {
			if fixed+n+n/255+1 == space {
				target = n
				break
			}
		}
		if target < 0 {
			continue
		}
		p, err := PadOpus(packets[last], target)
		if err != nil {
			continue
		}
		padded = p
		break
	}
	if padded == nil {
		return errors.New("cannot align Opus page")
	}
	packets[last] = padded
	if err := w.page(packets, w.granule, 0); err != nil {
		return err
	}
	w.pending = nil
	return nil
}
func (w *Writer) Close() error {
	if w.closed {
		return errors.New("TAF already closed")
	}
	if err := w.flush(); err != nil {
		return err
	}
	w.closed = true
	if !w.headers {
		return errors.New("TAF has no tracks")
	}
	h := &protocol.TonieboxAudioFileHeader{Sha1Hash: w.hash.Sum(nil), NumBytes: proto.Uint64(w.bytes), AudioId: proto.Uint32(w.id), TrackPageNums: w.tracks, XFill: []byte{}, OggGranulePosition: proto.Uint64(w.granule), OggPacketCount: proto.Uint64(w.packets), TafBlockNum: proto.Uint64(w.bytes / BlockSize), Pageno: proto.Uint64(uint64(w.seq))}
	data, err := MarshalHeader(h)
	if err != nil {
		return err
	}
	if _, err := w.out.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err = w.out.Write(data)
	return err
}

// StreamHeader writes a provisional, deterministic streaming header and restores
// the payload cursor. Close later replaces it with final length/hash/counters.
func (w *Writer) StreamHeader(payloadLimit uint64) error {
	data, err := MarshalHeader(&protocol.TonieboxAudioFileHeader{Sha1Hash: make([]byte, 20), NumBytes: proto.Uint64(payloadLimit), AudioId: proto.Uint32(w.id), XFill: []byte{}, OggGranulePosition: proto.Uint64(0), OggPacketCount: proto.Uint64(0), TafBlockNum: proto.Uint64(0), Pageno: proto.Uint64(0)})
	if err != nil {
		return err
	}
	position, err := w.out.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if _, err = w.out.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err = w.out.Write(data); err != nil {
		return err
	}
	_, err = w.out.Seek(position, io.SeekStart)
	return err
}
