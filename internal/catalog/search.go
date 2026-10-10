// SPDX-License-Identifier: GPL-2.0-only
package catalog

import (
	"encoding/json"
	"strings"
)

// Search preserves the reference's custom-first selection and shrinking quotas.
func (c *Catalog) Search(model, series, episode string) []map[string]any {
	active := c.active.Load()
	selected := make([]Entry, 0, 18)
	seen := map[string]bool{}
	for listIndex, list := range [][]Entry{active.Custom, active.Official} {
		limit := 9
		if listIndex == 1 {
			limit = 18
		}
		for fieldIndex, query := range []string{model, series, episode} {
			if query == "" {
				continue
			}
			if len(query) > 255 {
				query = query[:255]
			}
			field := []string{"model", "series", "episodes"}[fieldIndex]
			divisor := 3 - fieldIndex
			count := 0
			for _, entry := range list {
				if len(selected) >= limit || count >= (limit-len(selected))/divisor {
					break
				}
				key := strings.ToLower(entry.String("model"))
				if !seen[key] && strings.Contains(strings.ToLower(entry.String(field)), strings.ToLower(query)) {
					selected = append(selected, entry)
					seen[key] = true
					count++
				}
			}
		}
	}
	results := make([]map[string]any, 0, len(selected))
	for _, entry := range selected {
		tracks := []string{}
		_ = json.Unmarshal(entry["tracks"], &tracks)
		if tracks == nil {
			tracks = []string{}
		}
		results = append(results, map[string]any{"model": entry.String("model"), "series": entry.String("series"), "episode": entry.String("episodes"), "picture": entry.String("pic"), "language": entry.String("language"), "tracks": tracks})
	}
	return results
}
