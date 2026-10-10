// SPDX-License-Identifier: GPL-2.0-only
package catalog

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// Keep the original linear semantics as an independent oracle for indexed reads.
func linearAudioLookup(lists [][]Entry, id uint32, hash []byte) Entry {
	for _, list := range lists {
		for _, entry := range list {
			var ids []json.RawMessage
			if json.Unmarshal(entry["audio_id"], &ids) != nil {
				continue
			}
			matches := false
			for _, raw := range ids {
				var value string
				if json.Unmarshal(raw, &value) != nil {
					value = string(raw)
				}
				parsed, err := strconv.ParseUint(value, 10, 32)
				if err == nil && (uint32(parsed) == id || id < 0x50000000 && uint32(parsed) == id+0x50000000) {
					matches = true
					break
				}
			}
			if !matches {
				continue
			}
			var hashes []string
			if json.Unmarshal(entry["hash"], &hashes) != nil {
				continue
			}
			for _, encoded := range hashes {
				decoded, err := hex.DecodeString(encoded)
				if err == nil && len(decoded) == 20 && (hash == nil || bytes.Equal(decoded, hash)) {
					return entry
				}
			}
		}
	}
	return nil
}
func TestIndexesPreserveLinearLookupSemantics(t *testing.T) {
	var official, custom []Entry
	hashes := []string{strings.Repeat("01", 20), strings.Repeat("02", 20), "invalid", "00"}
	for index := range 128 {
		entry := Entry{}
		fields := map[string]any{
			"model":    fmt.Sprintf("Model_%d", index%19),
			"audio_id": []any{index % 13, fmt.Sprint(0x50000000 + index%17), index % 13, "invalid", -1},
			"hash":     []string{hashes[index%len(hashes)], hashes[(index+1)%len(hashes)]},
			"order":    index,
		}
		if index%7 == 0 {
			fields["hash"] = 123
		}
		if index%11 == 0 {
			fields["audio_id"] = "malformed"
		}
		for key, value := range fields {
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			entry[key] = data
		}
		if index%3 == 0 {
			custom = append(custom, entry)
		} else {
			official = append(official, entry)
		}
	}
	active := newSnapshot(official, custom, nil)
	catalog := &Catalog{}
	catalog.active.Store(active)
	for _, id := range []uint32{0, 1, 2, 8, 12, 13, 16, 17, 0x50000000, 0x50000008, 0x50000010, 0xffffffff} {
		for _, hash := range [][]byte{nil, bytes.Repeat([]byte{1}, 20), bytes.Repeat([]byte{2}, 20), make([]byte, 20), {1}} {
			expected := linearAudioLookup([][]Entry{custom, official}, id, hash)
			if got := catalog.ByAudio(id, hash); !reflect.DeepEqual(got, expected) {
				t.Fatalf("indexed audio differs for %x/%x", id, hash)
			}
		}
	}
	for index := range 21 {
		model := fmt.Sprintf("MODEL_%d", index)
		var expected Entry
		for _, list := range [][]Entry{custom, official} {
			for _, entry := range list {
				if expected == nil && strings.EqualFold(entry.String("model"), model) {
					expected = entry
				}
			}
		}
		if got := catalog.ByModel(model); !reflect.DeepEqual(got, expected) {
			t.Fatalf("indexed model differs for %q", model)
		}
	}
}
func TestModelIndexPreservesUnicodeEqualFold(t *testing.T) {
	for _, pair := range [][2]string{{"ſ", "S"}, {"K", "k"}, {"ς", "Σ"}, {"é", "É"}} {
		entry := Entry{"model": json.RawMessage(strconv.Quote(pair[0]))}
		catalog := &Catalog{}
		catalog.active.Store(newSnapshot([]Entry{entry}, nil, nil))
		if got := catalog.ByModel(pair[1]); got == nil {
			t.Fatalf("lost EqualFold match: %q", pair)
		}
	}
}
