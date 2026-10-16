// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"

	"goteddycloud/internal/config"
	"goteddycloud/internal/events"
)

func (s *Server) doCloud(ctx context.Context, snap, global config.Snapshot, method, path, query string, headers http.Header, body []byte) (*http.Response, error) {
	if !snap.Bool("cloud.enabled") {
		s.cloudBlocked.Add(1)
		return nil, errors.New("cloud disabled")
	}
	s.cloudRequests.Add(1)
	if s.MQTT != nil {
		s.MQTT.Event(events.NewEvent("", "CloudRequest", path))
		name := ""
		switch {
		case strings.HasPrefix(path, "/v1/time"):
			name = "LastCloudTime"
		case strings.Contains(path, "/ota/"):
			name = "LastCloudOtaTime"
		case strings.Contains(path, "/claim/"):
			name = "LastCloudClaimTime"
		case strings.Contains(path, "/freshness-check"):
			name = "LastCloudFreshnessCheckTime"
		case strings.Contains(path, "/cloud-reset"):
			name = "LastCloudResetTime"
		case strings.Contains(path, "/check-ota"):
			name = "LastCloudCheckOtaTime"
		case strings.Contains(path, "/setup-status"):
			name = "LastCloudSetupStatusTime"
		case strings.Contains(path, "/content-meta/"):
			name = "LastCloudContentMetaTime"
		case strings.Contains(path, "/chapter/"):
			name = "LastCloudChapterTime"
		case strings.Contains(path, "/content/"):
			name = "LastCloudContentTime"
		}
		if name != "" {
			s.MQTT.Record(snap.String("commonName"), name, s.Now().UTC().Format("2006-01-02T15:04:05Z"))
		}
	}
	response, err := s.Cloud.Do(ctx, snap, global, method, path, query, headers, body)
	// Record only route categories and status, never tag tokens, full URLs,
	// query strings or response bodies. This remains useful at the default log level.
	route := cloudLogRoute(path)
	if err != nil {
		s.cloudFailed.Add(1)
		slog.Warn("Tonies cloud request failed", "box", snap.String("commonName"), "method", method, "route", route, "reason", cloudLogReason(err))
	} else if response != nil {
		if response.StatusCode >= 300 {
			slog.Warn("Tonies cloud response", "box", snap.String("commonName"), "method", method, "route", route, "status", response.StatusCode)
		} else if strings.Contains(route, "/claim") || strings.Contains(route, "/content") {
			slog.Info("Tonies cloud response", "box", snap.String("commonName"), "method", method, "route", route, "status", response.StatusCode)
		}
	}
	if err == nil && response != nil && response.Body != nil {
		response.Body = &cloudBodyCounter{ReadCloser: response.Body, failed: &s.cloudFailed, onFailure: func() {
			slog.Warn("Tonies cloud audio transfer interrupted", "box", snap.String("commonName"), "route", route)
		}}
	}
	return response, err
}
func (s *Server) statsAPI(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != "GET" || !strings.HasPrefix(r.URL.Path, "/api/stats") {
		return false
	}
	values := []uint64{s.connections.Load(), s.reverseRequests.Load(), s.cloudRequests.Load(), s.cloudBlocked.Load(), s.cloudFailed.Load()}
	names := []string{"connections", "reverse_requests", "cloud_requests", "cloud_blocked", "cloud_failed"}
	descriptions := []string{"Connections made to this server", "Reverse proxy calls made by clients", "Cloud requests executed", "Blocked cloud requests", "Failed cloud requests"}
	rows := make([]map[string]any, 5)
	for i, name := range names {
		rows[i] = map[string]any{"ID": name, "description": descriptions[i], "value": values[i]}
	}
	jsonReply(w, "text/json", map[string]any{"stats": rows})
	return true
}

type cloudBodyCounter struct {
	io.ReadCloser
	failed    *atomic.Uint64
	recorded  atomic.Bool
	onFailure func()
}

func (c *cloudBodyCounter) Read(data []byte) (int, error) {
	n, err := c.ReadCloser.Read(data)
	if err != nil && err != io.EOF && c.recorded.CompareAndSwap(false, true) {
		c.failed.Add(1)
		if c.onFailure != nil {
			c.onFailure()
		}
	}
	return n, err
}

// Unknown routes are not copied verbatim into diagnostics because their path
// components can contain credentials supplied by a caller.
func cloudLogRoute(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) < 2 {
		return "other"
	}
	switch parts[0] {
	case "v1", "v2", "v3":
	default:
		return "other"
	}
	switch parts[1] {
	case "time", "claim", "content", "content-meta", "chapter", "freshness-check", "ota", "check-ota", "cloud-reset", "setup-status":
		return "/" + parts[0] + "/" + parts[1]
	}
	return "other"
}
func cloudLogReason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "request_canceled"
	}
	switch err.Error() {
	case "complete upstream CA/client identity required", "upstream client material unavailable":
		return "missing_client_certificate_or_key"
	case "invalid upstream CA", "invalid upstream client identity":
		return "invalid_client_certificate_or_key"
	case "invalid upstream target":
		return "invalid_cloud_host_or_port"
	case "upstream TLS or transport failure":
		return "tls_or_network_failure"
	case "upstream concurrency limit reached":
		return "too_many_cloud_requests"
	}
	return "request_failed"
}
