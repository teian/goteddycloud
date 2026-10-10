// SPDX-License-Identifier: GPL-2.0-only
package taf

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"google.golang.org/protobuf/proto"

	"goteddycloud/internal/protocol"
)

const BlockSize = 4096

func ReadHeader(r io.Reader) (*protocol.TonieboxAudioFileHeader, error) {
	var length [4]byte
	if _, err := io.ReadFull(r, length[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(length[:])
	if size == 0 || size > BlockSize-4 {
		return nil, errors.New("invalid TAF header length")
	}
	data := make([]byte, int(size))
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	h := new(protocol.TonieboxAudioFileHeader)
	if err := proto.Unmarshal(data, h); err != nil {
		return nil, fmt.Errorf("TAF protobuf: %w", err)
	}
	if len(h.Sha1Hash) != sha1.Size {
		return nil, errors.New("invalid TAF SHA1 length")
	}
	return h, nil
}
func MarshalHeader(h *protocol.TonieboxAudioFileHeader) ([]byte, error) {
	h = proto.Clone(h).(*protocol.TonieboxAudioFileHeader)
	// The required fill field must make the message exactly one header block.
	// Searching a 4092-byte range is bounded and handles protobuf length transitions.
	h.XFill = []byte{}
	if err := proto.CheckInitialized(h); err != nil {
		return nil, err
	}
	base := proto.Size(h)
	if base > BlockSize-4 {
		return nil, errors.New("TAF metadata exceeds header")
	}
	for size := BlockSize - 4 - base; size >= 0; size-- {
		h.XFill = make([]byte, size)
		if proto.Size(h) != BlockSize-4 {
			continue
		}
		data, err := proto.Marshal(h)
		if err != nil {
			return nil, err
		}
		out := make([]byte, 4, BlockSize)
		binary.BigEndian.PutUint32(out, uint32(len(data)))
		return append(out, data...), nil
	}
	return nil, errors.New("TAF fill cannot satisfy exact header size")
}
func Validate(r io.Reader) (*protocol.TonieboxAudioFileHeader, error) {
	h, err := ReadHeader(r)
	if err != nil {
		return nil, err
	}
	hash := sha1.New()
	n, err := io.Copy(hash, r)
	if err != nil {
		return nil, err
	}
	if uint64(n) != h.GetNumBytes() || !bytes.Equal(hash.Sum(nil), h.Sha1Hash) {
		return nil, errors.New("TAF payload hash/length mismatch")
	}
	return h, nil
}
