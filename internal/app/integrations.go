// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"

	"path/filepath"
	"runtime"
	"strconv"

	"goteddycloud/internal/content"
	"goteddycloud/internal/mqtt"
)

func (s *Server) StartIntegrations(parent context.Context, version string) error {
	ctx, cancel := context.WithCancel(parent)
	s.integrationsStop = cancel
	s.MQTT.Version = version
	s.MQTT.Hardware = runtime.GOOS + " " + runtime.GOARCH
	snap := s.Settings.Snapshot("")
	if snap.Bool("mqtt_server.enabled") {
		path := func(key string) string {
			value := snap.String(key)
			if !filepath.IsAbs(value) {
				value = filepath.Join(snap.Base, value)
			}
			return value
		}
		read := func(name string) ([]byte, error) {
			file, err := s.Settings.Storage().Open(name)
			if err != nil {
				return nil, err
			}
			defer file.Close()
			data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
			if err != nil || len(data) > 1<<20 {
				return nil, fmt.Errorf("MQTT certificate bounds")
			}
			return data, nil
		}
		crt, err := read(path("mqtt_server.cert.crt"))
		if err != nil {
			return fmt.Errorf("MQTT server certificate unavailable")
		}
		key, err := read(path("mqtt_server.cert.key"))
		if err != nil {
			return fmt.Errorf("MQTT server key unavailable")
		}
		pair, err := tls.X509KeyPair(crt, key)
		if err != nil {
			return fmt.Errorf("MQTT server certificate unavailable or invalid")
		}
		listener, err := net.Listen("tcp", net.JoinHostPort("", strconv.FormatUint(snap.Uint("mqtt_server.port"), 10)))
		if err != nil {
			return fmt.Errorf("MQTT server bind: %w", err)
		}
		server := &mqtt.Server{TLS: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}, SessionTicketsDisabled: true, ClientAuth: tls.RequestClientCert}, Known: func(id string) bool {
			for _, box := range s.Settings.BoxIDs() {
				if id == box {
					return true
				}
			}
			return false
		}, Fresh: func(id string) []string {
			target, err := s.boxServer(id)
			if err != nil {
				return nil
			}
			target.mu.Lock()
			defer target.mu.Unlock()
			items := make([]string, 0, len(target.freshness[id]))
			for _, uid := range target.freshness[id] {
				items = append(items, content.RUID(uid))
			}
			return items
		}}
		s.integrations.Add(1)
		go func() {
			defer s.integrations.Done()
			if err := server.Serve(ctx, listener); err != nil && ctx.Err() == nil {
				slog.Error("embedded MQTT server stopped unexpectedly")
			}
		}()
	}

	if snap.Bool("core.tonies_json_auto_update") {
		s.integrations.Add(1)
		go func() {
			defer s.integrations.Done()
			if err := s.updateCatalog(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("startup metadata update failed; previous catalog retained")
			}
			if ctx.Err() == nil {
				if err := s.updateBoxCatalog(ctx); err != nil && ctx.Err() == nil {
					slog.Warn("startup box metadata update failed; previous catalog retained")
				}
			}
		}()
	}
	s.integrations.Add(1)
	go func() { defer s.integrations.Done(); s.MQTT.Run(ctx) }()
	return nil
}
