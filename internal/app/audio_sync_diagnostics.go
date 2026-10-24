// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	"goteddycloud/internal/content"
	"goteddycloud/internal/events"
	"goteddycloud/internal/protocol"
)

type audioSyncTraceKey struct{}

type audioSyncDecision struct {
	RUID         string   `json:"ruid"`
	Source       string   `json:"source"`
	NoCloud      bool     `json:"nocloud"`
	BoxAudioID   uint32   `json:"boxAudioId"`
	LocalAudioID *uint32  `json:"localAudioId,omitempty"`
	LocalState   string   `json:"localState"`
	Pending      bool     `json:"pending"`
	Marked       bool     `json:"marked"`
	Reasons      []string `json:"reasons"`
}

type audioSyncTrace struct {
	started   time.Time
	decisions []audioSyncDecision
}

func newAudioSyncTrace(ctx context.Context) (context.Context, *audioSyncTrace) {
	trace := &audioSyncTrace{started: time.Now()}
	return context.WithValue(ctx, audioSyncTraceKey{}, trace), trace
}

// Remote URLs can contain credentials. Only expose confined local URI paths;
// omit arbitrary paths, URL authorities, queries, headers and raw error text.
func audioSyncSource(source string) string {
	if source == "" {
		return "tag-cache"
	}
	for _, prefix := range []string{"lib://", "content://", "data://", "lib-global://", "content-global://", "data-global://"} {
		if strings.HasPrefix(source, prefix) {
			if len(source) > 512 {
				return prefix + "[long path]"
			}
			return source
		}
	}
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		return "remote-stream"
	}
	return "local-path"
}

func audioSyncLocalState(err error) string {
	switch {
	case err == nil:
		return "ready"
	case errors.Is(err, os.ErrNotExist):
		return "missing"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "canceled"
	default:
		return cacheFailureReason(err)
	}
}

func (s *Server) publishAudioSync(box string, data map[string]any) {
	root := s
	if s.parent != nil {
		root = s.parent
	}
	data["serverVersion"] = root.MQTT.Version
	s.Events.Publish(events.NewEvent(box, "audio-sync", data))
}

func (s *Server) traceFreshness(box, route string, trace *audioSyncTrace, status int, reply *protocol.TonieFreshnessCheckResponse) {
	marked := make([]string, 0)
	markedUIDs := map[string]bool{}
	if reply != nil {
		for _, uid := range reply.TonieMarked {
			ruid := content.RUID(uid)
			marked = append(marked, ruid)
			markedUIDs[ruid] = true
		}
	}
	for i := range trace.decisions {
		decision := &trace.decisions[i]
		if markedUIDs[decision.RUID] && !decision.Marked {
			decision.Reasons = append(decision.Reasons, "cloud")
		}
		decision.Marked = markedUIDs[decision.RUID]
	}
	duration := time.Since(trace.started).Milliseconds()
	slog.Info("Box audio synchronization", "box", box, "route", route, "status", status,
		"requested", len(trace.decisions), "marked", len(marked), "duration_ms", duration)
	// Stay below the stream's 64 KiB frame limit, including large box libraries.
	// One summary is still emitted for empty or rejected freshness requests.
	for part := 0; ; part++ {
		start := min(part*16, len(trace.decisions))
		end := min(start+16, len(trace.decisions))
		markStart := min(part*128, len(marked))
		markEnd := min(markStart+128, len(marked))
		s.publishAudioSync(box, map[string]any{
			"phase": "freshness-response", "route": route, "status": status,
			"startedAt": trace.started.UTC().Format(time.RFC3339Nano), "durationMs": duration,
			"requested": len(trace.decisions), "markedCount": len(marked),
			"markedRuids": marked[markStart:markEnd], "markOffset": markStart,
			"offset": start, "tags": trace.decisions[start:end],
		})
		if end == len(trace.decisions) && markEnd == len(marked) {
			break
		}
	}
}
