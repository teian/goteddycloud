// SPDX-License-Identifier: GPL-2.0-only
// Custom model validation/normalization follows pinned handler_api.c; see NOTICE.
package catalog

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

func rawValue(raw json.RawMessage) any       { var value any; _ = json.Unmarshal(raw, &value); return value }
func put(entry Entry, key string, value any) { entry[key], _ = json.Marshal(value) }
func rawArray(entry Entry, key string) ([]json.RawMessage, bool) {
	raw, present := entry[key]
	if !present {
		return []json.RawMessage{}, true
	}
	if !strings.HasPrefix(strings.TrimSpace(string(raw)), "[") {
		return nil, false
	}
	var values []json.RawMessage
	err := json.Unmarshal(raw, &values)
	return values, err == nil
}
func audioNumber(raw json.RawMessage) (uint32, bool) {
	switch value := rawValue(raw).(type) {
	case float64:
		if value >= 0 && value <= math.MaxUint32 {
			return uint32(value), true
		}
	case string:
		n, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimLeft(value, " \t\r\n"), "+"), 10, 32)
		return uint32(n), err == nil
	}
	return 0, false
}
func normalizeCustom(entries []Entry) ([]Entry, error) {
	result := make([]Entry, len(entries))
	models := map[string]int{}
	pairs := map[string]int{}
	for index, original := range entries {
		fail := func(message string) error {
			return mutationError(InvalidMutation, "Invalid entry at index %d: %s", index, message)
		}
		if original == nil {
			return nil, fail("expected object")
		}
		entry := clone(original)
		for _, key := range []string{"model", "series"} {
			value, ok := rawValue(entry[key]).(string)
			if !ok || value == "" {
				return nil, fail("'" + key + "' is required")
			}
		}
		for _, key := range []string{"no", "title", "episodes", "language", "category", "pic"} {
			if raw, present := entry[key]; present {
				if _, ok := rawValue(raw).(string); !ok {
					return nil, fail("'" + key + "' must be string")
				}
			}
		}
		if raw, present := entry["release"]; present {
			switch rawValue(raw).(type) {
			case string, float64:
			default:
				return nil, fail("'release' must be string or numeric")
			}
		}
		tracks, ok := rawArray(entry, "tracks")
		if !ok {
			return nil, fail("'tracks' must be array")
		}
		trackStrings := make([]string, len(tracks))
		for i, raw := range tracks {
			value, ok := rawValue(raw).(string)
			if !ok {
				return nil, fail(fmt.Sprintf("'tracks[%d]' must be string", i))
			}
			trackStrings[i] = value
		}
		ids, idsOK := rawArray(entry, "audio_id")
		hashes, hashesOK := rawArray(entry, "hash")
		if !idsOK || !hashesOK {
			return nil, fail("'audio_id' and 'hash' must be arrays")
		}
		if len(ids) != len(hashes) {
			return nil, fail("'audio_id' and 'hash' must have same length")
		}
		normalizedIDs := make([]string, len(ids))
		normalizedHashes := make([]string, len(ids))
		for i, raw := range ids {
			id, ok := audioNumber(raw)
			if !ok {
				return nil, fail(fmt.Sprintf("'audio_id[%d]' must be numeric", i))
			}
			hash, ok := rawValue(hashes[i]).(string)
			decoded, err := hex.DecodeString(hash)
			if !ok || err != nil || len(decoded) != 20 || len(hash) != 40 {
				return nil, fail(fmt.Sprintf("'hash[%d]' must be 40-char hex", i))
			}
			normalizedIDs[i] = strconv.FormatUint(uint64(id), 10)
			normalizedHashes[i] = strings.ToUpper(hash)
		}
		model := entry.String("model")
		folded := strings.ToLower(model)
		if previous, ok := models[folded]; ok {
			return nil, mutationError(InvalidMutation, "Duplicate custom model '%s' at indexes %d and %d", model, previous, index)
		}
		models[folded] = index
		for i, id := range normalizedIDs {
			key := id + "|" + normalizedHashes[i]
			if previous, ok := pairs[key]; ok {
				return nil, mutationError(InvalidMutation, "Duplicate audio_id+hash pair detected (audio_id=%s) at indexes %d and %d", id, previous, index)
			}
			pairs[key] = index
		}
		for _, key := range []string{"no", "model", "title", "series", "episodes", "release", "language", "category", "pic"} {
			value := ""
			switch field := rawValue(entry[key]).(type) {
			case string:
				value = field
			case float64:
				value = fmt.Sprintf("%.0f", field)
			}
			put(entry, key, value)
		}
		put(entry, "tracks", trackStrings)
		put(entry, "audio_id", normalizedIDs)
		put(entry, "hash", normalizedHashes)
		result[index] = entry
	}
	return result, nil
}
func (c *Catalog) ReadCustom() ([]Entry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.read("tonies.custom.json")
}

func (c *Catalog) Upsert(entries []Entry, keep uint64, now time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pendingRename() {
		return mutationError(RenamePending, "Pending custom model rename must be recovered")
	}
	root, err := c.read("tonies.custom.json")
	if err != nil {
		return mutationError(CatalogReadFailure, "Failed to load tonies.custom.json")
	}
	for _, entry := range entries {
		if entry == nil {
			return mutationError(InvalidMutation, "Invalid payload: each entry must be object")
		}
		model := entry.String("model")
		if model == "" {
			return mutationError(InvalidMutation, "Invalid payload: 'model' is required")
		}
		replaced := false
		for i, old := range root {
			if strings.EqualFold(old.String("model"), model) {
				root[i] = clone(entry)
				replaced = true
				break
			}
		}
		if !replaced {
			root = append(root, clone(entry))
		}
	}
	return c.saveCustom(root, keep, now)
}
func (c *Catalog) Delete(models []string, keep uint64, now time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pendingRename() {
		return mutationError(RenamePending, "Pending custom model rename must be recovered")
	}
	root, err := c.read("tonies.custom.json")
	if err != nil {
		return mutationError(CatalogReadFailure, "Failed to load tonies.custom.json")
	}
	remaining := make([]Entry, 0, len(root))
	for _, entry := range root {
		remove := false
		for _, model := range models {
			if strings.EqualFold(entry.String("model"), model) {
				remove = true
				break
			}
		}
		if !remove {
			remaining = append(remaining, entry)
		}
	}
	return c.saveCustom(remaining, keep, now)
}
func (c *Catalog) saveCustom(entries []Entry, keep uint64, now time.Time) error {
	normalized, err := normalizeCustom(entries)
	if err != nil {
		return err
	}
	data, err := json.Marshal(normalized)
	if err != nil || len(data) > 64<<20 {
		return mutationError(CatalogWriteFailure, "Failed to encode tonies.custom.json")
	}
	if keep > 100 {
		return mutationError(InvalidMutation, "Invalid backup retention")
	}
	if old, err := c.root.ReadFile("tonies.custom.json"); err == nil && keep > 0 {
		name := "tonies.custom.json." + now.Format("20060102-150405") + ".bak"
		if err := c.root.WriteAtomic(name, old, 0600); err != nil {
			return mutationError(CatalogBackupFailure, "Failed to back up tonies.custom.json")
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return mutationError(CatalogReadFailure, "Failed to load tonies.custom.json")
	}
	if err := c.root.WriteAtomic("tonies.custom.json", data, 0600); err != nil {
		return mutationError(CatalogWriteFailure, "Failed to save tonies.custom.json")
	}
	active := c.active.Load()
	c.active.Store(newSnapshot(active.Official, normalized, active.V2))
	dir, err := c.root.Open(".")
	if err != nil {
		return nil
	}
	defer dir.Close()
	files, err := dir.ReadDir(-1)
	if err != nil {
		return nil
	}
	var backups []string
	for _, file := range files {
		if !file.IsDir() && strings.HasPrefix(file.Name(), "tonies.custom.json.") && strings.HasSuffix(file.Name(), ".bak") {
			backups = append(backups, file.Name())
		}
	}
	sort.Strings(backups)
	if len(backups) > int(keep) {
		for _, name := range backups[:len(backups)-int(keep)] {
			_ = c.root.Remove(name)
		}
	}
	return nil
}
