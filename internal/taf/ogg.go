// SPDX-License-Identifier: GPL-2.0-only
package taf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// OggCRC uses the non-reflected polynomial required by the Ogg page format.
func OggCRC(data []byte) uint32 {
	var crc uint32
	for _, b := range data {
		crc ^= uint32(b) << 24
		for range 8 {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04c11db7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

type OggReader struct {
	r        io.Reader
	pending  []byte
	packets  [][]byte
	serial   uint32
	sequence uint32
	started  bool
	ended    bool
}

func NewOggReader(r io.Reader) *OggReader { return &OggReader{r: r} }
func (r *OggReader) Next() ([]byte, error) {
	for len(r.packets) == 0 {
		var header [27]byte
		if _, err := io.ReadFull(r.r, header[:]); err != nil {
			if err == io.EOF && len(r.pending) > 0 {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		if !bytes.Equal(header[:4], []byte("OggS")) || header[4] != 0 {
			return nil, errors.New("invalid Ogg page")
		}
		if header[5]&^byte(7) != 0 {
			return nil, errors.New("invalid Ogg flags")
		}
		serial, seq := binary.LittleEndian.Uint32(header[14:]), binary.LittleEndian.Uint32(header[18:])
		if r.ended {
			return nil, errors.New("chained Ogg streams require explicit track boundaries")
		}
		if !r.started {
			if header[5]&2 == 0 || seq != 0 {
				return nil, errors.New("missing Ogg beginning")
			}
			r.serial = serial
			r.started = true
		} else if serial != r.serial || seq != r.sequence+1 {
			return nil, errors.New("invalid Ogg serial/sequence")
		}
		r.sequence = seq
		if (header[5]&1 != 0) != (len(r.pending) > 0) {
			return nil, errors.New("invalid Ogg packet continuation")
		}
		lacing := make([]byte, int(header[26]))
		if _, err := io.ReadFull(r.r, lacing); err != nil {
			return nil, err
		}
		size := 0
		for _, n := range lacing {
			size += int(n)
		}
		body := make([]byte, size)
		if _, err := io.ReadFull(r.r, body); err != nil {
			return nil, err
		}
		expected := binary.LittleEndian.Uint32(header[22:])
		clear(header[22:26])
		page := append(append(append([]byte{}, header[:]...), lacing...), body...)
		if OggCRC(page) != expected {
			return nil, errors.New("invalid Ogg checksum")
		}
		pos := 0
		for _, n := range lacing {
			if len(r.pending)+int(n) > 1<<20 {
				return nil, errors.New("exceeded Ogg packet limit")
			}
			r.pending = append(r.pending, body[pos:pos+int(n)]...)
			pos += int(n)
			if n < 255 {
				r.packets = append(r.packets, r.pending)
				r.pending = nil
			}
		}
		if header[5]&4 != 0 {
			r.ended = true
			if len(r.pending) > 0 {
				return nil, errors.New("truncated final Ogg packet")
			}
		}
	}
	packet := r.packets[0]
	r.packets = r.packets[1:]
	return packet, nil
}

func Page(packets [][]byte, serial, sequence uint32, granule uint64, flags byte) ([]byte, error) {
	var lace, body []byte
	for _, packet := range packets {
		remaining := len(packet)
		for remaining >= 255 {
			lace = append(lace, 255)
			remaining -= 255
		}
		lace = append(lace, byte(remaining))
		body = append(body, packet...)
	}
	if len(lace) > 255 {
		return nil, errors.New("exceeded Ogg segment limit")
	}
	out := make([]byte, 27, len(body)+len(lace)+27)
	copy(out, "OggS")
	out[5] = flags
	binary.LittleEndian.PutUint64(out[6:], granule)
	binary.LittleEndian.PutUint32(out[14:], serial)
	binary.LittleEndian.PutUint32(out[18:], sequence)
	out[26] = byte(len(lace))
	out = append(out, lace...)
	out = append(out, body...)
	binary.LittleEndian.PutUint32(out[22:], OggCRC(out))
	return out, nil
}

func CheckPages(r io.Reader) error {
	reader := NewOggReader(r)
	for {
		_, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("invalid Ogg integrity: %w", err)
		}
	}
}
