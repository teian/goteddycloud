// SPDX-License-Identifier: GPL-2.0-only
package catalog

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"unicode"
)

type audioMatch struct {
	entry  Entry
	hashes [][]byte
	rank   int
}

// foldModel canonicalizes the Unicode simple-fold classes used by EqualFold,
// including sigma, long-s and Kelvin-sign variants that ToLower alone misses.
func foldModel(model string) string {
	return strings.Map(func(char rune) rune {
		smallest := char
		for next := unicode.SimpleFold(char); next != char; next = unicode.SimpleFold(next) {
			if next < smallest {
				smallest = next
			}
		}
		return smallest
	}, model)
}

// newSnapshot builds immutable indexes before publication. Entries retain their
// custom-first, original-file ordering; duplicate IDs never change precedence.
func newSnapshot(official, custom, v2 []Entry) *snapshot {
	next := &snapshot{Official: official, Custom: custom, V2: v2, models: make(map[string]Entry), audio: make(map[uint32][]audioMatch)}
	rank := 0
	for _, list := range [][]Entry{custom, official} {
		for _, entry := range list {
			if model := entry.String("model"); model != "" {
				key := foldModel(model)
				if _, exists := next.models[key]; !exists {
					next.models[key] = entry
				}
			}
			ids := entryAudioIDs(entry)
			hashes := entryHashes(entry)
			if len(hashes) > 0 {
				for _, id := range ids {
					next.audio[id] = append(next.audio[id], audioMatch{entry: entry, hashes: hashes, rank: rank})
				}
			}
			rank++
		}
	}
	return next
}
func entryAudioIDs(entry Entry) []uint32 {
	var rawIDs []json.RawMessage
	if json.Unmarshal(entry["audio_id"], &rawIDs) != nil {
		return nil
	}
	ids := make([]uint32, 0, len(rawIDs))
	seen := make(map[uint32]bool, len(rawIDs))
	for _, raw := range rawIDs {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			text = string(raw)
		}
		value, err := strconv.ParseUint(text, 10, 32)
		if err == nil && !seen[uint32(value)] {
			ids = append(ids, uint32(value))
			seen[uint32(value)] = true
		}
	}
	return ids
}
func entryHashes(entry Entry) [][]byte {
	var encoded []string
	if json.Unmarshal(entry["hash"], &encoded) != nil {
		return nil
	}
	hashes := make([][]byte, 0, len(encoded))
	for _, text := range encoded {
		decoded, err := hex.DecodeString(text)
		if err == nil && len(decoded) == 20 {
			hashes = append(hashes, decoded)
		}
	}
	return hashes
}
func firstAudioMatch(matches []audioMatch, hash []byte) *audioMatch {
	for index := range matches {
		match := &matches[index]
		if hash == nil {
			return match
		}
		for _, candidate := range match.hashes {
			if bytes.Equal(candidate, hash) {
				return match
			}
		}
	}
	return nil
}
