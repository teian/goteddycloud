// SPDX-License-Identifier: GPL-2.0-only
package catalog

import (
	"encoding/json"
	"fmt"
	"testing"
)

func BenchmarkCatalogLookup(b *testing.B) {
	for _, size := range []int{100, 10000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			catalog, err := Open(b.TempDir())
			if err != nil {
				b.Fatal(err)
			}
			defer catalog.Close()
			entries := make([]map[string]any, size)
			for index := range entries {
				entries[index] = map[string]any{"model": fmt.Sprintf("MODEL_%05d", index), "audio_id": []uint32{uint32(index) + 0x50000000}, "hash": []string{"00112233445566778899aabbccddeeff00112233"}, "title": "Benchmark title"}
			}
			data, err := json.Marshal(entries)
			if err != nil {
				b.Fatal(err)
			}
			if err := catalog.ReplaceOfficial(data); err != nil {
				b.Fatal(err)
			}
			for _, kind := range []string{"ModelLast", "AudioLast", "AudioMissing"} {
				b.Run(kind, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						var entry Entry
						switch kind {
						case "ModelLast":
							entry = catalog.ByModel(fmt.Sprintf("MODEL_%05d", size-1))
						case "AudioLast":
							entry = catalog.ByAudio(uint32(size-1), nil)
						default:
							entry = catalog.ByAudio(0xfffffffe, nil)
						}
						if (entry == nil) != (kind == "AudioMissing") {
							b.Fatal("incorrect lookup")
						}
					}
				})
			}
		})
	}
}
