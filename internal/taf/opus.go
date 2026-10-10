// SPDX-License-Identifier: GPL-2.0-only
// Opus packet framing follows RFC 6716; padding structure was cross-checked
// against libopus repacketizer.c (Xiph.Org/Jean-Marc Valin), see third_party notices.
package taf

import (
	"errors"
	"fmt"
)

func frameSize(data []byte) (int, int, error) {
	if len(data) < 1 {
		return 0, 0, errors.New("truncated Opus size")
	}
	if data[0] < 252 {
		return int(data[0]), 1, nil
	}
	if len(data) < 2 {
		return 0, 0, errors.New("truncated extended Opus size")
	}
	return int(data[0]) + 4*int(data[1]), 2, nil
}
func putSize(n int) []byte {
	if n < 252 {
		return []byte{byte(n)}
	}
	first := 252 + (n & 3)
	return []byte{byte(first), byte((n - first) / 4)}
}

func OpusFrames(packet []byte) (byte, [][]byte, error) {
	if len(packet) < 1 {
		return 0, nil, errors.New("empty Opus packet")
	}
	toc := packet[0]
	data := packet[1:]
	count := 1
	vbr := false
	var sizes []int
	switch toc & 3 {
	case 0:
		sizes = []int{len(data)}
	case 1:
		count = 2
		if len(data)%2 != 0 {
			return 0, nil, errors.New("unequal CBR frames")
		}
		sizes = []int{len(data) / 2, len(data) / 2}
	case 2:
		count = 2
		n, used, err := frameSize(data)
		if err != nil {
			return 0, nil, err
		}
		data = data[used:]
		sizes = []int{n, len(data) - n}
	case 3:
		if len(data) < 1 {
			return 0, nil, errors.New("missing Opus control")
		}
		control := data[0]
		data = data[1:]
		count = int(control & 63)
		vbr = control&128 != 0
		if count < 1 || count > 48 {
			return 0, nil, errors.New("invalid Opus frame count")
		}
		if control&64 != 0 {
			padding := 0
			for {
				if len(data) == 0 {
					return 0, nil, errors.New("truncated Opus padding")
				}
				p := data[0]
				data = data[1:]
				if p == 255 {
					padding += 254
				} else {
					padding += int(p)
					break
				}
			}
			if padding > len(data) {
				return 0, nil, errors.New("oversized Opus padding")
			}
			data = data[:len(data)-padding]
		}
		if vbr {
			total := 0
			for i := 0; i < count-1; i++ {
				n, used, err := frameSize(data)
				if err != nil {
					return 0, nil, err
				}
				data = data[used:]
				sizes = append(sizes, n)
				total += n
			}
			sizes = append(sizes, len(data)-total)
		} else {
			if len(data)%count != 0 {
				return 0, nil, errors.New("invalid Opus CBR size")
			}
			for range count {
				sizes = append(sizes, len(data)/count)
			}
		}
	}
	frames := make([][]byte, 0, count)
	for _, size := range sizes {
		if size < 0 || size > 1275 || size > len(data) {
			return 0, nil, errors.New("invalid Opus frame size")
		}
		frames = append(frames, data[:size])
		data = data[size:]
	}
	if len(data) != 0 {
		return 0, nil, errors.New("unexpected Opus trailing data")
	}
	if OpusSamples(toc)*len(frames) > 5760 {
		return 0, nil, errors.New("invalid Opus packet duration exceeds 120ms")
	}
	return toc, frames, nil
}

// OpusSamples returns the 48kHz frame duration encoded in the TOC.
func OpusSamples(toc byte) int {
	if toc&128 != 0 {
		return 120 << ((toc >> 3) & 3)
	}
	if toc&96 == 96 {
		return 480 << ((toc >> 3) & 1)
	}
	n := (toc >> 3) & 3
	if n == 3 {
		return 2880
	}
	return 480 << n
}

func PadOpus(packet []byte, target int) ([]byte, error) {
	if target < len(packet) || target > 65535 {
		return nil, errors.New("invalid Opus padding target")
	}
	if target == len(packet) {
		return append([]byte(nil), packet...), nil
	}
	toc, frames, err := OpusFrames(packet)
	if err != nil {
		return nil, err
	}
	vbr := false
	for _, f := range frames {
		if len(f) != len(frames[0]) {
			vbr = true
		}
	}
	control := byte(len(frames))
	if vbr {
		control |= 128
	}
	sizes := []byte{}
	if vbr {
		for _, f := range frames[:len(frames)-1] {
			sizes = append(sizes, putSize(len(f))...)
		}
	}
	minimum := 2 + len(sizes)
	for _, f := range frames {
		minimum += len(f)
	}
	extra := target - minimum
	if extra < 0 {
		return nil, fmt.Errorf("insufficient Opus padding: need %d bytes", minimum)
	}
	out := make([]byte, 0, target)
	out = append(out, toc&252|3, control)
	if extra > 0 {
		out[1] |= 64
		large := (extra - 1) / 255
		for range large {
			out = append(out, 255)
		}
		out = append(out, byte(extra-large*255-1))
	}
	out = append(out, sizes...)
	for _, f := range frames {
		out = append(out, f...)
	}
	out = append(out, make([]byte, target-len(out))...)
	return out, nil
}
