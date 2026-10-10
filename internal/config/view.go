// SPDX-License-Identifier: GPL-2.0-only
package config

// View is an immutable point-in-time view of effective settings. It remains
// valid after settings change. Use Snapshot when an editable Values map is needed.
type View struct{ snapshot Snapshot }

// View reads effective settings without copying a cached map. Unknown overlays
// are resolved without retaining a cache entry.
func (s *Store) View(id string) View {
	snapshot, _ := s.snapshot(id)
	return View{snapshot: snapshot}
}
func (view View) String(key string) string { return view.snapshot.String(key) }
func (view View) Bool(key string) bool     { return view.snapshot.Bool(key) }
func (view View) Uint(key string) uint64   { return view.snapshot.Uint(key) }
