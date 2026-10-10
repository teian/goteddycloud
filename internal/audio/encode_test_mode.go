// SPDX-License-Identifier: GPL-2.0-only
package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

// EncodeTest follows the C test's 100 blocks of 333333 stereo samples and
// 0xDEAFBEEF identity. Its chapter limit leaves the final two blocks together.
func EncodeTest(ctx context.Context, destination string) error {
	folder, err := os.MkdirTemp("", "goteddycloud-encode-test-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(folder)
	const samples = 333333
	sources := make([]string, 99)
	total := 0
	for chapter := range sources {
		if err := ctx.Err(); err != nil {
			return err
		}
		frames := samples
		if chapter == 98 {
			frames *= 2
		}
		path := filepath.Join(folder, fmt.Sprintf("%02d.wav", chapter))
		file, err := os.Create(path)
		if err != nil {
			return err
		}
		var header [44]byte
		copy(header[:], "RIFF")
		binary.LittleEndian.PutUint32(header[4:], uint32(36+frames*4))
		copy(header[8:], "WAVEfmt ")
		binary.LittleEndian.PutUint32(header[16:], 16)
		binary.LittleEndian.PutUint16(header[20:], 1)
		binary.LittleEndian.PutUint16(header[22:], 2)
		binary.LittleEndian.PutUint32(header[24:], 48000)
		binary.LittleEndian.PutUint32(header[28:], 192000)
		binary.LittleEndian.PutUint16(header[32:], 4)
		binary.LittleEndian.PutUint16(header[34:], 16)
		copy(header[36:], "data")
		binary.LittleEndian.PutUint32(header[40:], uint32(frames*4))
		_, err = file.Write(header[:])
		var buffer [4096]byte
		for frame := 0; err == nil && frame < frames; {
			count := min(len(buffer)/4, frames-frame)
			for i := range count {
				sample := float32(total)
				left := int16(8192 * math.Sin(float64(sample/10*(1+float32(math.Sin(float64(sample/100000)))))))
				right := int16(8192 * math.Sin(float64(sample/20*(1+float32(math.Sin(float64(sample/30000)))))))
				binary.LittleEndian.PutUint16(buffer[i*4:], uint16(left))
				binary.LittleEndian.PutUint16(buffer[i*4+2:], uint16(right))
				total++
			}
			if err = ctx.Err(); err != nil {
				break
			}
			_, err = file.Write(buffer[:count*4])
			frame += count
		}
		closeErr := file.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		sources[chapter] = path
	}
	return Convert(ctx, destination, 0xdeafbeef, sources)
}
