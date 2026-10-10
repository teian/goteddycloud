// SPDX-License-Identifier: GPL-2.0-only
package boxcommands

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"

	"goteddycloud/internal/config"
	"goteddycloud/internal/events"
)

type Service struct {
	Settings *config.Store
	Events   *events.Hub
	mu       sync.Mutex
	streams  map[string]map[*stream]struct{}
}
type stream struct{ cancel context.CancelFunc }

func New(settings *config.Store, hub *events.Hub) *Service {
	return &Service{Settings: settings, Events: hub, streams: map[string]map[*stream]struct{}{}}
}
func Setting(command string) (key, event string, max uint64, boolean bool) {
	switch command {
	case "CmdVolLimitSpk", "toniebox.max_vol_spk":
		return "toniebox.max_vol_spk", "vol_limit_spk", 3, false
	case "CmdVolLimitHdp", "toniebox.max_vol_hdp":
		return "toniebox.max_vol_hdp", "vol_limit_hdp", 3, false
	case "CmdLed", "toniebox.led":
		return "toniebox.led", "led", 2, false
	case "CmdSlapEnabled", "toniebox.slap_enabled":
		return "toniebox.slap_enabled", "slap_enabled", 0, true
	case "CmdSlapDirection", "toniebox.slap_back_left":
		return "toniebox.slap_back_left", "slap_dir", 0, true
	}
	return "", "", 0, false
}
func (s *Service) Execute(id, command, payload string) error {
	found := false
	for _, box := range s.Settings.BoxIDs() {
		if box == id {
			found = true
			break
		}
	}
	if id == "" || !found {
		return errors.New("unknown box overlay")
	}
	if command == "CmdStop" {
		if !s.Stop(id) {
			return errors.New("no active stream")
		}
		return nil
	}
	key, event, maximum, boolean := Setting(command)
	if key == "" {
		return errors.New("unsupported box command")
	}
	value := payload
	if boolean {
		switch {
		case strings.EqualFold(payload, "TRUE"):
			value = "true"
		case strings.EqualFold(payload, "FALSE"):
			value = "false"
		default:
			return errors.New("invalid boolean command")
		}
	} else {
		number, err := strconv.ParseUint(payload, 10, 32)
		if err != nil || number > maximum {
			return errors.New("box command out of range")
		}
		value = strconv.FormatUint(number, 10)
	}
	if err := s.Settings.Set(id, key, value); err != nil {
		return err
	}
	s.Events.Publish(events.NewEvent(id, "cmd", event))
	return nil
}

// Track binds only server-owned work to a box; stop does not invent device pause/volume controls.
func (s *Service) Track(parent context.Context, id string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	entry := &stream{cancel}
	s.mu.Lock()
	if s.streams[id] == nil {
		s.streams[id] = map[*stream]struct{}{}
	}
	s.streams[id][entry] = struct{}{}
	s.mu.Unlock()
	return ctx, func() {
		cancel()
		s.mu.Lock()
		delete(s.streams[id], entry)
		if len(s.streams[id]) == 0 {
			delete(s.streams, id)
		}
		s.mu.Unlock()
	}
}
func (s *Service) Stop(id string) bool {
	s.mu.Lock()
	entries := s.streams[id]
	delete(s.streams, id)
	s.mu.Unlock()
	for entry := range entries {
		entry.cancel()
	}
	if len(entries) > 0 {
		s.Events.Publish(events.NewEvent(id, "cmd", "stop"))
		return true
	}
	return false
}

// Begin replaces server-owned streams for one box, without sending a device
// command or a synthetic stop event when a replacement request arrives.
func (s *Service) Begin(parent context.Context, id string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	entry := &stream{cancel}
	s.mu.Lock()
	old := s.streams[id]
	s.streams[id] = map[*stream]struct{}{entry: {}}
	s.mu.Unlock()
	for previous := range old {
		previous.cancel()
	}
	return ctx, func() {
		cancel()
		s.mu.Lock()
		delete(s.streams[id], entry)
		if len(s.streams[id]) == 0 {
			delete(s.streams, id)
		}
		s.mu.Unlock()
	}
}

func (s *Service) Cancel(id string) {
	s.mu.Lock()
	entries := s.streams[id]
	delete(s.streams, id)
	s.mu.Unlock()
	for entry := range entries {
		entry.cancel()
	}
}
