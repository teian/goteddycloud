// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"goteddycloud/internal/config"
)

func TestReadableLogFieldsAndInjectionProtection(t *testing.T) {
	var output bytes.Buffer
	handler := newReadableHandler(&output, slog.LevelDebug)
	record := slog.NewRecord(time.Date(2026, 10, 16, 20, 10, 14, 428000000, time.UTC), slog.LevelInfo, "Tonies audio download completed", 0)
	record.Add("box", "6CC374F2CBD8", "tag_prefix", "97763c1a", "status", 200, "bytes", int64(53745278), "elapsed_ms", int64(72), "cache_to_library", true,
		"source", "/data/audio_with_underscores.taf", "reason", "retry_backoff", "content_length", int64(-1), "error", "bad\nforged | Status: OK\x1b[31m")
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	line := output.String()
	for _, want := range []string{"2026-10-16 20:10:14.428Z [INFO] Tonies audio download completed", "Box: 6CC374F2CBD8", "Tag prefix: 97763c1a", "HTTP status: 200 OK", "Transferred: 51.26 MiB (53745278 bytes)", "Elapsed: 72 ms", "Cache to library: yes", "Source: /data/audio_with_underscores.taf", "Reason: retry backoff", "Content length: unknown", `Error: "bad\nforged | Status: OK\x1b[31m"`} {
		if !strings.Contains(line, want) {
			t.Fatalf("missing %q in %s", want, line)
		}
	}
	if strings.Count(line, "\n") != 1 {
		t.Fatal("one event must occupy exactly one line", line)
	}
}

func TestReadableLoggerGroupsAndConcurrentWriters(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(newReadableHandler(&output, slog.LevelInfo)).With("box", "ABC")
	grouped := logger.WithGroup("cache").With("stage", "publish")
	var writers sync.WaitGroup
	for range 20 {
		writers.Go(func() {
			grouped.Info("Published", slog.Group("audio", "bytes", 1024), slog.Attr{}, slog.Group("empty"))
		})
	}
	writers.Wait()
	logger.Info("Ungrouped", "status", 401)
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 21 {
		t.Fatal("concurrent log events interleaved", output.String())
	}
	for _, line := range lines[:20] {
		if !strings.HasSuffix(line, " | Box: ABC | Cache / Stage: publish | Cache / Audio / Transferred: 1.00 KiB (1024 bytes)") {
			t.Fatal("lost or incorrectly grouped attributes", line)
		}
	}
	if !strings.HasSuffix(lines[20], " | Box: ABC | HTTP status: 401 Unauthorized") {
		t.Fatal("derived logger mutated parent", lines[20])
	}
}

func TestStartupBannerSurvivesQuietLoggingAndLevelChanges(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	defer slog.SetDefault(previous)
	var level slog.LevelVar
	level.Set(slog.Level(100))
	configureLogging(&output, &level, "dev-test123")
	slog.Info("Hidden")
	if lines := strings.Split(strings.TrimSpace(output.String()), "\n"); len(lines) != 1 || !strings.HasSuffix(lines[0], "[INFO] GoTeddyCloud starting | Name: GoTeddyCloud | Version: dev-test123") {
		t.Fatal("startup banner must be first and always visible", output.String())
	}
	level.Set(slog.LevelInfo)
	slog.Debug("Hidden certificate details")
	slog.Info("Visible result")
	level.Set(slog.LevelDebug)
	slog.Debug("Visible certificate details")
	if strings.Contains(output.String(), "Hidden") || !strings.Contains(output.String(), "[INFO] Visible result") || !strings.Contains(output.String(), "[DEBUG] Visible certificate details") {
		t.Fatal("runtime log level changes not honored", output.String())
	}
}

func TestConfiguredLogLevelUpdatesWithoutRestart(t *testing.T) {
	settings, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	handler := newReadableHandler(&bytes.Buffer{}, configuredLevel{settings})
	for _, test := range []struct {
		setting string
		level   slog.Level
		want    bool
	}{{"4", slog.LevelDebug, false}, {"4", slog.LevelInfo, true}, {"5", slog.LevelDebug, true}, {"3", slog.LevelInfo, false}, {"3", slog.LevelWarn, true}, {"0", slog.LevelError, false}} {
		if err := settings.Set("", "log.level", test.setting); err != nil {
			t.Fatal(err)
		}
		if got := handler.Enabled(context.Background(), test.level); got != test.want {
			t.Fatalf("log.level=%s level=%s enabled=%v, want %v", test.setting, test.level, got, test.want)
		}
	}
}
