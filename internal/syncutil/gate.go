// SPDX-License-Identifier: GPL-2.0-only
// Package syncutil provides cancellable transaction coordination.
package syncutil

import (
	"context"
	"sync"
)

// RWGate is a cancellable, writer-preferring read/write gate. Its zero value is
// ready for use. Release functions must be called exactly once. Like RWMutex,
// it does not support recursive acquisition or upgrading a reader to a writer.
type RWGate struct {
	mu             sync.Mutex
	readers        int
	writer         bool
	waitingWriters int
	changed        chan struct{}
}

func (g *RWGate) signalLocked() {
	if g.changed != nil {
		close(g.changed)
	}
	g.changed = make(chan struct{})
}

func (g *RWGate) Lock(ctx context.Context) (func(), error)  { return g.acquire(ctx, true) }
func (g *RWGate) RLock(ctx context.Context) (func(), error) { return g.acquire(ctx, false) }
func (g *RWGate) acquire(ctx context.Context, write bool) (func(), error) {
	g.mu.Lock()
	if g.changed == nil {
		g.changed = make(chan struct{})
	}
	if write {
		g.waitingWriters++
	}
	for {
		if err := ctx.Err(); err != nil {
			if write {
				g.waitingWriters--
				g.signalLocked()
			}
			g.mu.Unlock()
			return nil, err
		}
		if !g.writer && ((write && g.readers == 0) || (!write && g.waitingWriters == 0)) {
			if write {
				g.waitingWriters--
				g.writer = true
			} else {
				g.readers++
			}
			g.mu.Unlock()
			return func() {
				g.mu.Lock()
				if write {
					g.writer = false
				} else {
					g.readers--
				}
				g.signalLocked()
				g.mu.Unlock()
			}, nil
		}
		changed := g.changed
		g.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-changed:
		}
		g.mu.Lock()
	}
}
