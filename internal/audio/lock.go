// SPDX-License-Identifier: GPL-2.0-only
package audio

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

type destinationEntry struct {
	token chan struct{}
	refs  int
}

var destinations = struct {
	sync.Mutex
	entries map[string]*destinationEntry
}{entries: map[string]*destinationEntry{}}

// Serialize read-modify-publish append jobs as well as replacement conversions.
// Entries disappear after their last waiter, so historical filenames do not grow
// the process's memory. Waiting consumes admission, but no encoder subprocess.
func lockDestination(ctx context.Context, path string) (func(), error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	directory, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return nil, err
	}
	key := filepath.Join(directory, filepath.Base(absolute))
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	destinations.Lock()
	entry := destinations.entries[key]
	if entry == nil {
		entry = &destinationEntry{token: make(chan struct{}, 1)}
		destinations.entries[key] = entry
	}
	entry.refs++
	destinations.Unlock()
	releaseRef := func() {
		destinations.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(destinations.entries, key)
		}
		destinations.Unlock()
	}
	select {
	case entry.token <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-entry.token
			releaseRef()
			return nil, err
		}
		return func() { <-entry.token; releaseRef() }, nil
	case <-ctx.Done():
		releaseRef()
		return nil, ctx.Err()
	}
}
