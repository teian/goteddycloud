// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"log/slog"

	"goteddycloud/internal/config"
)

type configuredLevel struct{ settings *config.Store }

func (l configuredLevel) Level() slog.Level {
	switch l.settings.Snapshot("").Uint("log.level") {
	case 0, 1:
		return slog.Level(100)
	case 2:
		return slog.LevelError
	case 3:
		return slog.LevelWarn
	case 4:
		return slog.LevelInfo
	default:
		return slog.LevelDebug
	}
}
