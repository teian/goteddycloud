// SPDX-License-Identifier: GPL-2.0-only
package config

import (
	"errors"
	"fmt"
)

// Reload validates both flat files before publishing either configuration map.
// Runtime identity/state fields are retained; listeners are not rebound here.
func (s *Store) Reload() error {
	if s.readOnly {
		return errors.New("read-only configuration")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := &Store{root: s.root, global: map[string]string{}, overlays: map[string]map[string]string{}}
	if err := next.load("config.ini", false); err != nil {
		return err
	}
	if err := next.load("config.overlay.ini", true); err != nil {
		return err
	}
	validate := func(values map[string]string) error {
		for key, value := range values {
			if d, ok := s.defs[key]; ok {
				if err := d.Validate(value); err != nil {
					return fmt.Errorf("invalid persisted setting %s", key)
				}
			}
		}
		return nil
	}
	if err := validate(next.global); err != nil {
		return err
	}
	for _, values := range next.overlays {
		if err := validate(values); err != nil {
			return err
		}
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.global = next.global
	s.overlays = next.overlays
	s.invalidateLocked("")
	return nil
}

func (s *Store) Persist() error {
	if s.readOnly {
		return errors.New("read-only configuration")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	overlay := map[string]string{}
	for id, values := range s.overlays {
		for key, value := range values {
			overlay["overlay."+id+"."+key] = value
		}
	}
	if err := s.root.WriteAtomic("config.ini", marshal(s.global), 0600); err != nil {
		return err
	}
	return s.root.WriteAtomic("config.overlay.ini", marshal(overlay), 0600)
}
