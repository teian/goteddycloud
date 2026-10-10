// SPDX-License-Identifier: GPL-2.0-only
package config

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPublishedSettingsReadersDoNotWaitForMutation(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Set("box", "toniebox.max_vol_spk", "1"); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if s.String("box", "toniebox.max_vol_spk") != "1" || !s.HasBox("box") {
			t.Error("wrong published state")
		}
		if len(s.BoxIDs()) != 1 || !s.IsOverlayed("box", "toniebox.max_vol_spk") {
			t.Error("wrong overlay state")
		}
		_ = s.Snapshot("unknown")
		_ = s.View("")
		if err := s.SetRuntime("box", map[string]string{"internal.toniebox_firmware.rtnlVersion": "runtime progressed"}); err != nil {
			t.Error(err)
		}
		if err := s.Validate(); err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-done:
		s.mu.Unlock()
	case <-time.After(time.Second):
		s.mu.Unlock()
		<-done
		t.Fatal("readers waited for persistence mutex")
	}
}

func TestConcurrentDurableAndRuntimePublication(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	old := s.View("")
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("box%d", i)
			for j := range 8 {
				if err := s.Set(id, "toniebox.max_vol_spk", fmt.Sprint(j%4)); err != nil {
					t.Error(err)
					return
				}
				if err := s.SetRuntime(id, map[string]string{"internal.toniebox_firmware.rtnlVersion": fmt.Sprint(j)}); err != nil {
					t.Error(err)
					return
				}
				_ = s.View(id)
				_ = s.Snapshot("unknown")
			}
		}()
	}
	wg.Wait()
	if len(s.BoxIDs()) != 8 {
		t.Fatal("lost durable updates")
	}
	for _, id := range s.BoxIDs() {
		v := s.View(id)
		if v.Uint("toniebox.max_vol_spk") != 3 || v.String("internal.toniebox_firmware.rtnlVersion") != "7" {
			t.Fatal("lost runtime or persisted update", id)
		}
	}
	if old.Uint("toniebox.max_vol_spk") != 3 {
		t.Fatal("previous snapshot changed")
	}
}

// A mixed workload exercises cheap read views during runtime and durable writes.
func BenchmarkSettingsMixed(b *testing.B) {
	s, err := Open(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	var operations atomic.Uint64
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		n := operations.Add(31)
		for pb.Next() {
			n++
			switch {
			case n%1024 == 0:
				if err := s.Set("box", "toniebox.max_vol_spk", fmt.Sprint(n/1024%4)); err != nil {
					b.Error(err)
				}
			case n%128 == 0:
				if err := s.SetRuntime("box", map[string]string{"internal.toniebox_firmware.rtnlVersion": fmt.Sprint(n)}); err != nil {
					b.Error(err)
				}
			default:
				if s.View("").String("core.datadir") == "" {
					b.Error("missing data path")
				}
			}
		}
	})
}

func TestFailedPersistenceRetainsDurableStateAndRuntimeCanProgress(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set("", "toniebox.max_vol_spk", "1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRuntime("", map[string]string{"internal.toniebox_firmware.rtnlVersion": "before"}); err != nil {
		t.Fatal(err)
	}
	old := s.View("")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("", "toniebox.max_vol_spk", "2"); err == nil {
		t.Fatal("write to closed root succeeded")
	}
	if err := s.SetRuntime("", map[string]string{"internal.toniebox_firmware.rtnlVersion": "after"}); err != nil {
		t.Fatal(err)
	}
	now := s.View("")
	if now.Uint("toniebox.max_vol_spk") != 1 || now.String("internal.toniebox_firmware.rtnlVersion") != "after" {
		t.Fatal("failed persistence replaced state or prevented runtime publication")
	}
	if old.String("internal.toniebox_firmware.rtnlVersion") != "before" {
		t.Fatal("previous published state mutated")
	}
}
