// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"strconv"
	"strings"

	"goteddycloud/internal/config"
	"goteddycloud/internal/content"
	"goteddycloud/internal/events"
)

// Commit discovery before announcing it: a browser can fetch a new unknown tag
// immediately, without waiting for the box to request an audio download.
func (s *Server) tagPresented(ctx context.Context, snap config.Snapshot, id, ruid string) {
	ruid = strings.ToLower(ruid)
	if _, err := content.UIDPath(ruid); err != nil || ruid == "0000000000000000" || ruid == "ffffffffffffffff" {
		return
	}
	if err := s.Content.UpdateContext(ctx, snap, ruid, func(m content.Metadata) error {
		m.Set("hide", false)
		return nil
	}); err != nil {
		return
	}
	now := s.Now().Unix()
	if err := s.Settings.SetRuntime(id, map[string]string{
		"internal.last_ruid": ruid, "internal.last_ruid_time": strconv.FormatInt(now, 10),
	}); err != nil {
		return
	}
	s.Events.Publish(events.NewEvent(id, "tag-present", map[string]any{
		"box": id, "ruid": ruid, "time": now,
	}))
}

func (s *Server) tagUpdated(id, ruid string) {
	s.Events.Publish(events.NewEvent(id, "tag-updated", map[string]string{
		"box": id, "ruid": strings.ToLower(ruid),
	}))
}

func (s *Server) tagState() []map[string]any {
	state := []map[string]any{}
	for _, id := range s.Settings.BoxIDs() {
		if s.parent != nil {
			owner, ok := s.parent.boxOwner(id)
			if !ok || owner.ID != s.areaID {
				continue
			}
		}
		snap := s.Settings.Snapshot(id)
		ruid := strings.ToLower(snap.String("internal.last_ruid"))
		if _, err := content.UIDPath(ruid); err != nil || ruid == "ffffffffffffffff" || ruid == "0000000000000000" {
			continue
		}
		state = append(state, map[string]any{"box": id, "ruid": ruid, "time": snap.Uint("internal.last_ruid_time")})
	}
	return state
}
