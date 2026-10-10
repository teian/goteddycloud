// SPDX-License-Identifier: GPL-2.0-only
package config

import (
	"bufio"
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"goteddycloud/internal/storage"
)

//go:embed schema.json
var schema []byte

type Definition struct {
	ID          string  `json:"ID"`
	Description string  `json:"description"`
	Label       string  `json:"label"`
	Internal    bool    `json:"internal"`
	Level       int     `json:"level"`
	Type        string  `json:"type"`
	Default     any     `json:"valueInit"`
	Min         float64 `json:"min"`
	Max         float64 `json:"max"`
}

// Schema defaults are scalars. Cache decoding while returning independent
// definition slices so callers cannot change the shared schema.
var schemaDefinitions = sync.OnceValue(func() []Definition {
	var definitions []Definition
	if err := json.Unmarshal(schema, &definitions); err != nil {
		panic(err)
	}
	return definitions
})

func Definitions() []Definition { return slices.Clone(schemaDefinitions()) }

type Store struct {
	// mu serializes durable mutations. Runtime changes do not wait for disk I/O.
	mu       sync.Mutex
	stateMu  sync.Mutex // publication and transient state only
	active   atomic.Pointer[settingsState]
	readOnly bool
	base     string
	root     *storage.Root
	backend  storage.Backend
	defs     map[string]Definition
	order    []Definition
	global   map[string]string
	overlays map[string]map[string]string
	runtime  map[string]map[string]string
}
type settingsState struct {
	cached   map[string]Snapshot
	global   map[string]string
	overlays map[string]map[string]string
	runtime  map[string]map[string]string
}

type Snapshot struct {
	backend storage.Backend
	Base    string
	Values  map[string]string
}

func Open(base string) (*Store, error) { return OpenWithStorage(base, storage.Filesystem{}, false) }

// OpenReadOnly never creates deployment files or permits subsequent mutations.
func OpenReadOnly(base string) (*Store, error) {
	return OpenWithStorage(base, storage.Filesystem{}, true)
}
func OpenWithStorage(base string, backend storage.Backend, readOnly bool) (*Store, error) {
	absolute, err := filepath.Abs(base)
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(absolute, "config")
	if !readOnly {
		if err := backend.MkdirAll(configPath, 0700); err != nil {
			return nil, err
		}
	}
	root, err := backend.OpenRoot(configPath)
	if err != nil {
		return nil, err
	}
	s := &Store{backend: backend, readOnly: readOnly, base: absolute, root: root, defs: map[string]Definition{}, order: Definitions(), global: map[string]string{}, overlays: map[string]map[string]string{}, runtime: map[string]map[string]string{}}
	for _, d := range s.order {
		s.defs[d.ID] = d
	}
	if err := s.load("config.ini", false); err != nil {
		root.Close()
		return nil, err
	}
	if err := s.load("config.overlay.ini", true); err != nil {
		root.Close()
		return nil, err
	}
	s.publishLocked("")
	return s, nil
}
func (s *Store) Close() error { return s.root.Close() }
func (s *Store) load(name string, overlay bool) error {
	data, err := s.root.ReadFile(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if overlay {
			rest, ok := strings.CutPrefix(key, "overlay.")
			if !ok {
				continue
			}
			id, field, ok := strings.Cut(rest, ".")
			if !ok || !ValidBoxID(id) {
				continue
			}
			if s.overlays[id] == nil {
				s.overlays[id] = map[string]string{}
			}
			s.overlays[id][field] = value
		} else {
			s.global[key] = value
		}
	}
	return scanner.Err()
}
func ValidBoxID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
func format(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return fmt.Sprint(x)
	}
}
func (d Definition) Validate(value string) error {
	if d.ID == "core.server.trusted_proxies" {
		for _, cidr := range strings.Fields(value) {
			if _, err := netip.ParsePrefix(cidr); err != nil {
				return fmt.Errorf("invalid proxy CIDR %q: %w", cidr, err)
			}
		}
	}
	switch d.Type {
	case "string":
		if strings.ContainsAny(value, "\n\r\x00") {
			return errors.New("multiline settings are not supported")
		}
	case "bool":
		if value != "true" && value != "false" {
			return errors.New("expected true or false")
		}
	case "uint", "hex":
		v, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return err
		}
		if d.Max > d.Min && (float64(v) < d.Min || float64(v) > d.Max) {
			return errors.New("value outside setting range")
		}
	case "int":
		v, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			return err
		}
		if d.Max > d.Min && (float64(v) < d.Min || float64(v) > d.Max) {
			return errors.New("value outside setting range")
		}
	case "float":
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return err
		}
		if math.IsNaN(v) || math.IsInf(v, 0) || d.Max > d.Min && (v < d.Min || v > d.Max) {
			return errors.New("value outside setting range")
		}
	default:
		return errors.New("setting is not editable")
	}
	return nil
}

// Snapshot returns an independently mutable copy of the effective settings.
// Cached maps remain private and immutable, including across invalidation.
func (s *Store) Snapshot(id string) Snapshot {
	snapshot, shared := s.snapshot(id)
	if shared {
		snapshot.Values = maps.Clone(snapshot.Values)
	}
	return snapshot
}

// String reads one effective setting without allocating a complete snapshot.
func (s *Store) String(id, key string) string {
	snapshot, _ := s.snapshot(id)
	return snapshot.String(key)
}

// Bool reads one effective boolean setting without copying the settings map.
func (s *Store) Bool(id, key string) bool { return s.String(id, key) == "true" }

// snapshot reports whether its map is shared and must be cloned by mutable callers.
func (s *Store) snapshot(id string) (Snapshot, bool) {
	state := s.active.Load()
	if cached, ok := state.cached[id]; ok {
		return cached, true
	}
	// Unknown IDs are deliberately not retained in the immutable index.
	values := maps.Clone(state.cached[""].Values)
	for key, value := range state.runtime[id] {
		values[key] = value
	}
	values["commonName"] = id
	snapshot := Snapshot{backend: s.backend, Base: s.base, Values: values}
	if len(state.runtime[id]) > 0 {
		snapshot.resolve()
	}
	return snapshot, false
}

// publishLocked requires stateMu after initialization. All maps reachable through active are immutable.
func (s *Store) publishLocked(id string) {
	var cached map[string]Snapshot
	previous := s.active.Load()
	if previous == nil || id == "" {
		cached = make(map[string]Snapshot, len(s.overlays)+1)
		cached[""] = s.buildSnapshotLocked("")
		for box := range s.overlays {
			cached[box] = s.buildSnapshotLocked(box)
		}
	} else {
		cached = maps.Clone(previous.cached)
		if s.overlays[id] != nil {
			cached[id] = s.buildSnapshotLocked(id)
		} else {
			delete(cached, id)
		}
	}
	runtime := make(map[string]map[string]string, len(s.runtime))
	for id, values := range s.runtime {
		runtime[id] = maps.Clone(values)
	}
	s.active.Store(&settingsState{cached: cached, global: s.global, overlays: maps.Clone(s.overlays), runtime: runtime})
}

func (s *Store) invalidateLocked(id string) { s.publishLocked(id) }
func (s *Store) buildSnapshotLocked(id string) Snapshot {
	values := make(map[string]string, len(s.order)+24)
	for _, d := range s.order {
		values[d.ID] = format(d.Default)
	}
	for k, v := range s.global {
		if d, ok := s.defs[k]; ok && d.Type != "" && d.Validate(v) != nil {
			continue
		}
		values[k] = v
	}
	for k, v := range s.overlays[id] {
		if d, ok := s.defs[k]; ok && d.Type != "" && d.Validate(v) != nil {
			continue
		}
		values[k] = v
	}
	for k, v := range s.runtime[""] {
		values[k] = v
	}
	for k, v := range s.runtime[id] {
		values[k] = v
	}
	if id != "" {
		values["commonName"] = id
	}
	values["internal.basedirfull"] = s.base
	values["internal.basedir"] = s.base
	snap := Snapshot{backend: s.backend, Base: s.base, Values: values}
	snap.resolve()
	return snap
}
func (v Snapshot) String(key string) string { return v.Values[key] }
func (v Snapshot) Bool(key string) bool     { return v.Values[key] == "true" }
func (v Snapshot) Uint(key string) uint64   { n, _ := strconv.ParseUint(v.Values[key], 10, 64); return n }
func resolve(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}
func (v Snapshot) resolve() {
	data := resolve(v.Base, v.String("core.datadir"))
	v.Values["internal.datadirfull"] = data
	v.Values["internal.configdirfull"] = resolve(v.Base, v.String("core.configdir"))
	v.Values["internal.certdirfull"] = resolve(v.Base, v.String("core.certdir"))
	for _, name := range []string{"library", "www", "firmware", "cache"} {
		v.Values["internal."+name+"dirfull"] = resolve(data, v.String("core."+name+"dir"))
	}
	content := resolve("content", v.String("core.contentdir"))
	v.Values["internal.contentdirfull"] = resolve(data, content)
	v.Values["internal.contentdirrel"] = resolve(v.String("core.datadir"), content)
	v.Values["internal.pluginsdirfull"] = resolve(v.String("internal.wwwdirfull"), v.String("core.pluginsdir"))
}
func (s *Store) Set(id, key, value string) error {
	return s.Update(id, map[string]*string{key: &value})
}
func (s *Store) Reset(id, key string) error { return s.Update(id, map[string]*string{key: nil}) }
func (s *Store) Update(id string, updates map[string]*string) error {
	if s.readOnly {
		return errors.New("read-only configuration")
	}
	if id != "" && !ValidBoxID(id) {
		return errors.New("invalid box ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, value := range updates {
		d, ok := s.defs[key]
		if !ok {
			return fmt.Errorf("unknown setting %s", key)
		}
		if value != nil {
			if err := d.Validate(*value); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
	}
	// Clone only the changed flat file; active snapshots stay unchanged on failure.
	var current map[string]string
	name := "config.ini"
	if id == "" {
		current = s.global
	} else {
		current = s.overlays[id]
		name = "config.overlay.ini"
	}
	changed := map[string]string{}
	for k, v := range current {
		changed[k] = v
	}
	for k, v := range updates {
		if v == nil {
			delete(changed, k)
		} else {
			changed[k] = *v
		}
	}
	if id != "" && len(changed) > 0 {
		if _, ok := changed["commonName"]; !ok {
			changed["commonName"] = id
		}
	}
	var disk map[string]string
	if id == "" {
		disk = changed
	} else {
		disk = map[string]string{}
		for box, values := range s.overlays {
			if box == id {
				continue
			}
			for k, v := range values {
				disk["overlay."+box+"."+k] = v
			}
		}
		for k, v := range changed {
			disk["overlay."+id+"."+k] = v
		}
	}
	data := marshal(disk)
	if err := s.root.WriteAtomic(name, data, 0600); err != nil {
		return err
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if id == "" {
		s.global = changed
	} else {
		if len(changed) == 0 {
			delete(s.overlays, id)
		} else {
			s.overlays[id] = changed
		}
	}
	s.invalidateLocked(id)
	return nil
}
func marshal(values map[string]string) []byte {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&out, "%s=%s\n", k, values[k])
	}
	return []byte(out.String())
}

// HasBox reads the current durable overlay index without taking a mutation lock.
func (s *Store) HasBox(id string) bool { _, ok := s.active.Load().overlays[id]; return ok }

func (s *Store) BoxIDs() []string {
	overlays := s.active.Load().overlays
	ids := make([]string, 0, len(overlays))
	for id := range overlays {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
func (s *Store) RemoveOverlay(id string) error {
	if s.readOnly {
		return errors.New("read-only configuration")
	}
	if !ValidBoxID(id) {
		return errors.New("invalid box ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	disk := map[string]string{}
	for box, values := range s.overlays {
		if box == id {
			continue
		}
		for k, v := range values {
			disk["overlay."+box+"."+k] = v
		}
	}
	if err := s.root.WriteAtomic("config.overlay.ini", marshal(disk), 0600); err != nil {
		return err
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	delete(s.overlays, id)
	delete(s.runtime, id)
	s.invalidateLocked(id)
	return nil
}
func (s *Store) Definition(key string) (Definition, bool) { d, ok := s.defs[key]; return d, ok }
func (s *Store) IsOverlayed(id, key string) bool {
	_, ok := s.active.Load().overlays[id][key]
	return ok
}

// Validate reports setting identifiers only: persisted secrets are never included.
func (s *Store) Validate() error {
	state := s.active.Load()
	var problems []error
	check := func(prefix string, values map[string]string) {
		for key, value := range values {
			if d, ok := s.defs[key]; ok && d.Type != "" {
				if d.Validate(value) != nil {
					problems = append(problems, fmt.Errorf("invalid persisted setting %s%s", prefix, key))
				}
			}
		}
	}
	check("", state.global)
	for id, values := range state.overlays {
		check("overlay."+id+".", values)
	}
	return errors.Join(problems...)
}

// SetRuntime updates transient internal fields and the detected box generation
// without writing configuration.
func (s *Store) SetRuntime(id string, values map[string]string) error {
	if s.readOnly {
		return errors.New("read-only configuration")
	}
	if id != "" && !ValidBoxID(id) {
		return errors.New("invalid box ID")
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	for key, value := range values {
		d, ok := s.defs[key]
		if !ok || (!d.Internal && key != "toniebox.boxGeneration") {
			return fmt.Errorf("not an internal setting: %s", key)
		}
		if err := d.Validate(value); err != nil {
			return err
		}
	}
	if s.runtime[id] == nil {
		s.runtime[id] = map[string]string{}
	}
	changed := false
	for key, value := range values {
		previous, present := s.runtime[id][key]
		if !present || previous != value {
			changed = true
			s.runtime[id][key] = value
		}
	}
	if changed {
		s.invalidateLocked(id)
	}
	return nil
}

func (s *Store) Storage() storage.Backend { return s.backend }
func (v Snapshot) Storage() storage.Backend {
	if v.backend == nil {
		return storage.Filesystem{}
	}
	return v.backend
}
