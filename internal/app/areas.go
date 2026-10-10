// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"goteddycloud/internal/config"
	"goteddycloud/internal/storage"
)

// Each area owns its settings, content, catalog, event hub and box state.
// Creation is published before I/O, allowing unrelated areas to initialize in parallel.
type userArea struct {
	ready   chan struct{}
	mu      sync.Mutex
	server  *Server
	backend *storage.Scoped
	err     error
}

func (a *userArea) close() error {
	<-a.ready
	if a.server == nil {
		return nil
	}
	return errors.Join(a.server.Close(), a.server.Settings.Close(), a.backend.Close())
}
func (s *Server) userArea(user webUser) (*Server, error) {
	record, ok := s.areaByID(user.AreaID)
	if !ok {
		return nil, errors.New("unknown default area")
	}
	return s.areaServer(record)
}
func (s *Server) areaServer(record webArea) (*Server, error) {
	key := record.ID
	s.areasMu.Lock()
	if s.areasClosed {
		s.areasMu.Unlock()
		return nil, os.ErrClosed
	}
	if s.areas == nil {
		s.areas = make(map[string]*userArea)
	}
	area, exists := s.areas[key]
	if !exists {
		area = &userArea{ready: make(chan struct{})}
		s.areas[key] = area
	}
	s.areasMu.Unlock()
	if !exists {
		area.server, area.backend, area.err = s.openArea(record)
		close(area.ready)
		if area.err != nil {
			// A temporary storage failure must not poison this area until restart.
			s.areasMu.Lock()
			if s.areas[key] == area {
				delete(s.areas, key)
			}
			s.areasMu.Unlock()
		}
	}
	<-area.ready
	if area.err != nil {
		return nil, area.err
	}
	area.mu.Lock()
	defer area.mu.Unlock()
	// Read current ownership after any concurrent account mutation.
	current, ok := s.areaByID(record.ID)
	if !ok {
		return nil, errors.New("area removed")
	}
	if err := s.syncAreaBoxes(area, current); err != nil {
		return nil, err
	}
	return area.server, nil
}
func inheritedSetting(key string) bool {
	return strings.HasPrefix(key, "cloud.") || strings.HasPrefix(key, "toniebox.") || strings.HasPrefix(key, "encode.") || strings.HasPrefix(key, "tonie_json.") || key == "core.allowPrivateFetch" || key == "core.boxCertAuth"
}
func (s *Server) openArea(record webArea) (*Server, *storage.Scoped, error) {
	// Opaque, stable identity survives password changes, but not account recreation.
	digest := sha256.Sum256([]byte(record.ID))
	base := filepath.Join(s.Settings.Snapshot("").Base, "users", hex.EncodeToString(digest[:]))
	backend, err := storage.NewScoped(s.Settings.Storage(), base)
	if err != nil {
		return nil, nil, err
	}
	settings, err := config.OpenWithStorage(base, backend, false)
	if err != nil {
		backend.Close()
		return nil, nil, err
	}
	updates := map[string]*string{}
	parent := s.Settings.Snapshot("")
	for _, d := range config.Definitions() {
		if !d.Internal && d.Type != "" && inheritedSetting(d.ID) {
			value := parent.String(d.ID)
			updates[d.ID] = &value
		}
	}
	no := "false"
	updates["core.allowNewBox"] = &no
	if err = settings.Update("", updates); err != nil {
		settings.Close()
		backend.Close()
		return nil, nil, err
	}
	snap := settings.Snapshot("")
	for _, path := range []string{snap.String("internal.contentdirfull"), snap.String("internal.librarydirfull"), snap.String("internal.cachedirfull"), snap.String("internal.wwwdirfull"), filepath.Join(snap.String("internal.wwwdirfull"), "custom_img")} {
		if err = backend.MkdirAll(path, 0700); err != nil {
			settings.Close()
			backend.Close()
			return nil, nil, err
		}
	}
	// Official metadata is shared information; custom catalogs remain private.
	if data, readErr := s.Catalog.ReadOfficial(); readErr == nil {
		root, rootErr := backend.OpenRoot(snap.String("internal.configdirfull"))
		if rootErr == nil {
			_, statErr := root.Stat("tonies.json")
			if errors.Is(statErr, os.ErrNotExist) {
				err = root.WriteAtomic("tonies.json", data, 0600)
			}
			root.Close()
		}
		if rootErr != nil || err != nil {
			settings.Close()
			backend.Close()
			return nil, nil, errors.Join(rootErr, err)
		}
	}
	child, err := New(settings)
	if err != nil {
		settings.Close()
		backend.Close()
		return nil, nil, err
	}
	child.parent = s
	child.areaID = record.ID
	child.Cloud = s.Cloud
	child.Now = s.Now
	// The administrator's broker remains the single external integration.
	child.MQTT = s.MQTT
	ctx, cancel := context.WithCancel(context.Background())
	child.integrationsStop = cancel
	child.integrations.Add(1)
	// Observe independently of SSE; a slow broker cannot block producers.
	channel, unsubscribe := child.Events.Observe()
	go func() {
		defer child.integrations.Done()
		defer unsubscribe()
		for {
			select {
			case <-ctx.Done():
				return
			case event, open := <-channel:
				if !open {
					return
				}
				if owner, ok := s.boxOwner(event.Box); ok && owner.ID == record.ID {
					s.MQTT.Event(event)
				}
			}
		}
	}()
	return child, backend, nil
}
func (s *Server) syncAreaBoxes(area *userArea, record webArea) error {
	settings := area.server.Settings
	for _, id := range settings.BoxIDs() {
		if !slices.Contains(record.Boxes, id) {
			if err := settings.RemoveOverlay(id); err != nil {
				return err
			}
		}
	}
	var shared []string
	for _, id := range record.Boxes {
		snap := s.Settings.Snapshot(id)
		updates := map[string]*string{}
		if !settings.HasBox(id) {
			for _, d := range config.Definitions() {
				if !d.Internal && d.Type != "" && (inheritedSetting(d.ID) || d.ID == "boxName" || d.ID == "boxModel" || d.ID == "commonName") {
					value := snap.String(d.ID)
					updates[d.ID] = &value
				}
			}
		}
		for _, key := range []string{"toniebox.api_access", "toniebox.boxGeneration", "core.boxCertAuth"} {
			if value := snap.String(key); settings.String(id, key) != value {
				updates[key] = &value
			}
		}
		for _, suffix := range []string{"ca", "crt", "key"} {
			key := "core.client_cert.file." + suffix
			path := snap.String(key)
			if !filepath.IsAbs(path) {
				path = filepath.Join(snap.Base, path)
			}
			shared = append(shared, path)
			if settings.String(id, key) != path {
				updates[key] = &path
			}
			key = "core.client_cert.data." + suffix
			if value := snap.String(key); settings.String(id, key) != value {
				updates[key] = &value
			}
		}
		if len(updates) > 0 {
			if err := settings.Update(id, updates); err != nil {
				return err
			}
		}
	}
	area.backend.AllowReadFiles(shared)
	return nil
}
func (s *Server) boxServer(id string) (*Server, error) {
	if s.parent == nil {
		if user, ok := s.boxOwner(id); ok {
			return s.areaServer(user)
		}
	}
	return s, nil
}
func (s *Server) executeBoxCommand(id, command, payload string) error {
	target, err := s.boxServer(id)
	if err != nil {
		return err
	}
	return target.Commands.Execute(id, command, payload)
}
