// SPDX-License-Identifier: GPL-2.0-only
package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"goteddycloud/internal/storage"
)

type Entry map[string]json.RawMessage

func (e Entry) String(key string) string {
	var value string
	_ = json.Unmarshal(e[key], &value)
	return value
}
func clone(entry Entry) Entry {
	if entry == nil {
		return nil
	}
	out := Entry{}
	for k, v := range entry {
		out[k] = append(json.RawMessage(nil), v...)
	}
	return out
}

type snapshot struct {
	Official, Custom, V2 []Entry
	models               map[string]Entry
	audio                map[uint32][]audioMatch
}
type Catalog struct {
	mu     sync.Mutex
	root   *storage.Root
	active atomic.Pointer[snapshot]
}

func Open(directory string) (*Catalog, error) {
	return OpenWithStorage(directory, storage.Filesystem{})
}
func OpenWithStorage(directory string, backend storage.Backend) (*Catalog, error) {
	root, err := backend.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	c := &Catalog{root: root}
	if err := c.Reload(); err != nil {
		root.Close()
		return nil, err
	}
	return c, nil
}
func (c *Catalog) Close() error { return c.root.Close() }
func (c *Catalog) read(name string) ([]Entry, error) {
	f, err := c.root.Open(name)
	if errors.Is(err, os.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 64<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 64<<20 {
		return nil, errors.New("catalog exceeds size limit")
	}
	return parse(name, data)
}

func parse(name string, data []byte) ([]Entry, error) {
	if len(data) > 64<<20 {
		return nil, errors.New("catalog exceeds size limit")
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("%s: malformed metadata", name)
	}
	if entries == nil {
		return nil, fmt.Errorf("%s: expected array", name)
	}
	for _, entry := range entries {
		if entry == nil {
			return nil, fmt.Errorf("%s: entry must be an object", name)
		}
	}
	return entries, nil
}

// ReadOfficial returns the original bytes, independently of the active lookup
// snapshot, matching the reference file-download endpoint.
func (c *Catalog) ReadOfficial() ([]byte, error) {
	f, err := c.root.Open("tonies.json")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 64<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 64<<20 {
		return nil, errors.New("catalog exceeds size limit")
	}
	return data, nil
}

// ReplaceOfficial validates the complete next snapshot before publishing either
// disk or memory. Custom mutations share this lock and cannot be lost.
func (c *Catalog) ReplaceOfficial(data []byte) error {
	official, err := parse("tonies.json", data)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	custom, err := c.read("tonies.custom.json")
	if err != nil {
		return err
	}
	v2, err := c.read("toniesV2.json")
	if err != nil {
		return err
	}
	if err := c.root.WriteAtomic("tonies.json", data, 0600); err != nil {
		return err
	}
	c.active.Store(newSnapshot(official, custom, v2))
	return nil
}

// Reload publishes all databases together; failed parses retain the last good snapshot.
func (c *Catalog) Reload() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	official, err := c.read("tonies.json")
	if err != nil {
		return err
	}
	custom, err := c.read("tonies.custom.json")
	if err != nil {
		return err
	}
	v2, err := c.read("toniesV2.json")
	if err != nil {
		return err
	}
	c.active.Store(newSnapshot(official, custom, v2))
	return nil
}
func (c *Catalog) Entries(custom bool) []Entry {
	s := c.active.Load()
	source := s.Official
	if custom {
		source = s.Custom
	}
	out := make([]Entry, len(source))
	for i, e := range source {
		out[i] = clone(e)
	}
	return out
}
func (c *Catalog) ByModel(model string) Entry {
	if model == "" {
		return nil
	}
	return clone(c.active.Load().models[foldModel(model)])
}

// ByAudio preserves custom-first file order across exact and teddyBench-adjusted
// IDs. IDs and hashes match independently within the same entry, as in C.
func (c *Catalog) ByAudio(id uint32, hash []byte) Entry {
	active := c.active.Load()
	match := firstAudioMatch(active.audio[id], hash)
	if id < 0x50000000 {
		adjusted := firstAudioMatch(active.audio[id+0x50000000], hash)
		if adjusted != nil && (match == nil || adjusted.rank < match.rank) {
			match = adjusted
		}
	}
	if match == nil {
		return nil
	}
	return clone(match.entry)
}

// ReplaceBoxes retains the official frontend's companion database verbatim.
func (c *Catalog) ReplaceBoxes(data []byte) error {
	if _, err := parse("tonieboxes.json", data); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.root.WriteAtomic("tonieboxes.json", data, 0600)
}
