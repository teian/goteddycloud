// SPDX-License-Identifier: GPL-2.0-only
package events

import (
	"testing"
)

func TestSlowSubscriberDisconnectAndCleanup(t *testing.T) {
	h := New()
	slow, stop, ok := h.Subscribe()
	if !ok {
		t.Fatal("subscribe")
	}
	defer stop()
	fast, end, ok := h.Subscribe()
	if !ok {
		t.Fatal("subscribe")
	}
	defer end()
	for i := 0; i < 65; i++ {
		h.Publish(NewEvent("box", "test", i))
		<-fast
	}
	n := 0
	for range slow {
		n++
	}
	if n != 64 {
		t.Fatal(n)
	}
	stop()
	end()
	for range 8 {
		_, _, ok := h.Subscribe()
		if !ok {
			t.Fatal("slots leaked")
		}
	}
	if _, _, ok := h.Subscribe(); ok {
		t.Fatal("subscriber limit ignored")
	}
}
