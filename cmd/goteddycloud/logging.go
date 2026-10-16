// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"goteddycloud/internal/config"
)

// readableHandler keeps each event on one line and shares its output lock with
// derived loggers, so concurrent downloads cannot interleave their messages.
type readableHandler struct {
	output *logOutput
	level  slog.Leveler
	fields []string
	groups []string
}

type logOutput struct {
	sync.Mutex
	writer io.Writer
}

func newReadableHandler(writer io.Writer, level slog.Leveler) *readableHandler {
	return &readableHandler{output: &logOutput{writer: writer}, level: level}
}

func (h *readableHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *readableHandler) Handle(_ context.Context, record slog.Record) error {
	var line strings.Builder
	fmt.Fprintf(&line, "%s [%s] %s", record.Time.UTC().Format("2006-01-02 15:04:05.000Z"), record.Level, logText(upperFirst(record.Message)))
	fields := append([]string(nil), h.fields...)
	record.Attrs(func(attr slog.Attr) bool {
		fields = appendLogFields(fields, h.groups, attr)
		return true
	})
	for _, field := range fields {
		line.WriteString(" | ")
		line.WriteString(field)
	}
	line.WriteByte('\n')
	h.output.Lock()
	defer h.output.Unlock()
	_, err := io.WriteString(h.output.writer, line.String())
	return err
}

func (h *readableHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.fields = append([]string(nil), h.fields...)
	for _, attr := range attrs {
		clone.fields = appendLogFields(clone.fields, h.groups, attr)
	}
	return &clone
}

func (h *readableHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.groups = append(append([]string(nil), h.groups...), name)
	return &clone
}

func appendLogFields(fields, groups []string, attr slog.Attr) []string {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return fields
	}
	if attr.Value.Kind() == slog.KindGroup {
		if attr.Key != "" {
			groups = append(append([]string(nil), groups...), attr.Key)
		}
		for _, child := range attr.Value.Group() {
			fields = appendLogFields(fields, groups, child)
		}
		return fields
	}
	labels := make([]string, 0, len(groups)+1)
	for _, group := range groups {
		labels = append(labels, logLabel(group))
	}
	labels = append(labels, logLabel(attr.Key))
	return append(fields, logText(strings.Join(labels, " / "))+": "+logValue(attr))
}

func logLabel(key string) string {
	switch key {
	case "cloud_request":
		return "Request"
	case "tag_prefix":
		return "Tag prefix"
	case "status":
		return "HTTP status"
	case "elapsed_ms":
		return "Elapsed"
	case "bytes":
		return "Transferred"
	case "auth_state":
		return "Tag password"
	case "auth_source":
		return "Password source"
	case "saved_auth_state":
		return "Saved tag password"
	}
	words := strings.FieldsFunc(key, func(r rune) bool { return r == '_' || r == '.' })
	for i, word := range words {
		switch strings.ToLower(word) {
		case "http", "https", "tls", "mqtt", "ca", "cn", "sha256", "id", "ruid", "api", "url":
			words[i] = strings.ToUpper(word)
		default:
			if i == 0 {
				words[i] = upperFirst(word)
			}
		}
	}
	return strings.Join(words, " ")
}

func upperFirst(value string) string {
	letters := []rune(value)
	if len(letters) > 0 {
		letters[0] = unicode.ToUpper(letters[0])
	}
	return string(letters)
}

func logValue(attr slog.Attr) string {
	value := attr.Value
	if value.Kind() == slog.KindInt64 || value.Kind() == slog.KindUint64 {
		number := value.String()
		switch attr.Key {
		case "status":
			code, _ := strconv.Atoi(number)
			if text := http.StatusText(code); text != "" {
				return number + " " + text
			}
		case "elapsed_ms":
			return number + " ms"
		case "bytes", "content_length":
			amount, _ := strconv.ParseFloat(number, 64)
			if amount < 0 {
				return "unknown"
			}
			for i, unit := range []string{"GiB", "MiB", "KiB"} {
				size := float64(uint64(1) << (30 - 10*i))
				if amount >= size {
					return fmt.Sprintf("%.2f %s (%s bytes)", amount/size, unit, number)
				}
			}
			return number + " bytes"
		}
	}
	if value.Kind() == slog.KindBool {
		if value.Bool() {
			return "yes"
		}
		return "no"
	}
	text := value.String()
	if value.Kind() == slog.KindString {
		switch attr.Key {
		case "reason", "stage", "auth_source", "client_certificate", "saved_ruid", "user_agent_kind":
			text = strings.ReplaceAll(text, "_", " ")
		}
	}
	if text == "" {
		return "(none)"
	}
	return logText(text)
}

// Quote controls and delimiters to prevent user-supplied values from forging
// additional log lines or fields. Ordinary paths and names remain unquoted.
func logText(value string) string {
	if strings.Contains(value, " | ") || strings.ContainsAny(value, "\"\\") || strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || r == '\u2028' || r == '\u2029'
	}) {
		return strconv.Quote(value)
	}
	return value
}

func configureLogging(writer io.Writer, level slog.Leveler, buildVersion string) {
	handler := newReadableHandler(writer, level)
	slog.SetDefault(slog.New(handler))
	// Always identify the running build, even when routine logging is disabled.
	// Workers use stdout as a protocol stream and never call this function.
	banner := slog.NewRecord(time.Now(), slog.LevelInfo, "GoTeddyCloud starting", 0)
	banner.Add("name", "GoTeddyCloud", "version", buildVersion)
	_ = handler.Handle(context.Background(), banner)
}

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
