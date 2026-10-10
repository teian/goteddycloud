// SPDX-License-Identifier: GPL-2.0-only
package taf

import (
	"bytes"
	"crypto/sha1"
	"errors"
	"io"
)

// AppendWriter resumes a complete, block-aligned stream in a private output.
// Existing payload bytes and the audio ID remain unchanged. The pinned C
// append path resets the chapter table; new chapters start after the old payload.
func AppendWriter(out io.WriteSeeker, original io.ReadSeeker) (*Writer, error) {
	if _, err := original.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	h, err := Validate(original)
	if err != nil {
		return nil, err
	}
	if h.GetNumBytes()%BlockSize != 0 || len(h.TrackPageNums) == 0 {
		return nil, errors.New("TAF is not appendable")
	}
	for i, offset := range h.TrackPageNums {
		if uint64(offset)*BlockSize >= h.GetNumBytes() || i > 0 && offset <= h.TrackPageNums[i-1] {
			return nil, errors.New("invalid chapter offsets")
		}
	}
	if _, err := original.Seek(BlockSize, io.SeekStart); err != nil {
		return nil, err
	}
	reader := NewOggReader(original)
	head, err := reader.Next()
	if err != nil || len(head) != 19 || !bytes.Equal(head[:8], []byte("OpusHead")) {
		return nil, errors.New("missing OpusHead")
	}
	tags, err := reader.Next()
	if err != nil || !bytes.HasPrefix(tags, []byte("OpusTags")) {
		return nil, errors.New("missing OpusTags")
	}
	var granule uint64
	packets := uint64(2)
	for {
		packet, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		toc, frames, err := OpusFrames(packet)
		if err != nil {
			return nil, err
		}
		samples := OpusSamples(toc) * len(frames)
		if samples != 2880 {
			return nil, errors.New("expected 60ms append source")
		}
		granule += uint64(samples)
		packets++
	}
	if reader.ended || reader.serial != h.GetAudioId() {
		return nil, errors.New("TAF stream cannot be resumed")
	}
	// Metadata must agree with the stream, rather than trusting resume counters.
	if h.GetOggGranulePosition() != granule || h.GetOggPacketCount() != packets || h.GetPageno() != uint64(reader.sequence)+1 {
		return nil, errors.New("inconsistent TAF resume metadata")
	}
	if _, err := original.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if _, err := io.CopyN(out, original, BlockSize); err != nil {
		return nil, err
	}
	hash := sha1.New()
	if _, err := io.CopyN(io.MultiWriter(out, hash), original, int64(h.GetNumBytes())); err != nil {
		return nil, err
	}
	return &Writer{out: out, hash: hash, id: h.GetAudioId(), seq: reader.sequence + 1, bytes: h.GetNumBytes(), granule: granule, packets: packets, tracks: nil, headers: true}, nil
}
