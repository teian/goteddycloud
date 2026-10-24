// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"goteddycloud/internal/events"
)

func (s *Server) sse(w http.ResponseWriter, r *http.Request) {
	subscriber, unsubscribe, ok := s.Events.Subscribe()
	if !ok {
		response(w, 503, "", nil)
		return
	}
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	control := http.NewResponseController(w)
	send := func(event events.Event) error {
		if s.parent != nil && event.Box != "" {
			owner, ok := s.parent.boxOwner(event.Box)
			if !ok || owner.ID != s.areaID {
				return nil
			}
		}
		data, _ := json.Marshal(map[string]any{"type": event.Name, "data": event.Data, "box": event.Box})
		_ = control.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Name, data); err != nil {
			return err
		}
		err := control.Flush()
		// The idle interval exceeds the write budget. Do not leave an expired
		// deadline on the connection while waiting for the next event.
		_ = control.SetWriteDeadline(time.Time{})
		return err
	}
	keepalive := events.NewEvent("", "keep-alive", "")
	if err := send(keepalive); err != nil {
		return
	}
	// Subscribe before taking a snapshot so discoveries during reconnection
	// remain queued and are delivered after the initial state.
	if err := send(events.NewEvent("", "tag-state", s.tagState())); err != nil {
		return
	}
	timer := time.NewTicker(15 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-subscriber:
			if !open {
				return
			}
			if err := send(event); err != nil {
				return
			}
		case <-timer.C:
			if err := send(keepalive); err != nil {
				return
			}
		}
	}
}
