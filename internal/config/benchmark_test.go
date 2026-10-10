// SPDX-License-Identifier: GPL-2.0-only
package config

import "testing"

func BenchmarkSnapshot(b *testing.B) {
	store, err := Open(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	if err := store.Set("001122334455", "toniebox.max_vol_spk", "2"); err != nil {
		b.Fatal(err)
	}
	for _, tc := range []struct{ name, id string }{{"Global", ""}, {"Overlay", "001122334455"}, {"UnknownOverlay", "unknown"}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if store.Snapshot(tc.id).Uint("toniebox.max_vol_spk") == 0 {
					b.Fatal("missing setting")
				}
			}
		})
	}
	b.Run("Parallel", func(b *testing.B) {
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				store.Snapshot("001122334455")
			}
		})
	})
}
