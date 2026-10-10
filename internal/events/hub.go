// SPDX-License-Identifier: GPL-2.0-only
package events

import (
	"encoding/json"
	"sync"
)

type Event struct {
	Name, Box string
	Data      json.RawMessage
}

func NewEvent(box, name string, value any) Event {
	data, _ := json.Marshal(value)
	return Event{Name: name, Box: box, Data: data}
}

type Hub struct {
	mu          sync.Mutex
	subscribers map[chan Event]struct{}
	observers   map[chan Event]struct{}
}

func New() *Hub {
	return &Hub{subscribers: map[chan Event]struct{}{}, observers: map[chan Event]struct{}{}}
}
func (h *Hub) Subscribe() (<-chan Event, func(), bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.subscribers) >= 8 {
		return nil, nil, false
	}
	c := make(chan Event, 64)
	h.subscribers[c] = struct{}{}
	return c, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subscribers[c]; ok {
			delete(h.subscribers, c)
			close(c)
		}
	}, true
}

// Overflow disconnects only the slow subscriber; producers never wait for I/O.
func (h *Hub) Publish(event Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.subscribers {
		copy := event
		copy.Data = append(json.RawMessage(nil), event.Data...)
		select {
		case c <- copy:
		default:
			delete(h.subscribers, c)
			close(c)
		}
	}
	for c := range h.observers {
		copy := event
		copy.Data = append(json.RawMessage(nil), event.Data...)
		select {
		case c <- copy:
		default:
			delete(h.observers, c)
			close(c)
		}
	}
}

// Observe reserves an internal integration queue independently of the SSE limit.
func (h *Hub) Observe() (<-chan Event, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := make(chan Event, 512)
	h.observers[c] = struct{}{}
	return c, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.observers[c]; ok {
			delete(h.observers, c)
			close(c)
		}
	}
}
