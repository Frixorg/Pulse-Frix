// Package cache provides a bounded in-memory buffer for offline-first agent
// behaviour. When the control plane is unreachable the agent keeps monitoring
// locally and buffers a BOUNDED amount of data, dropping the OLDEST low-priority
// items first. Critical items are preferentially retained. See
// docs/AGENT_PROTOCOL.md#offline-behaviour and spec sections 71, 89.
package cache

import "sync"

// Priority ranks buffered items. Higher is more important.
type Priority int

const (
	PriorityHistorical Priority = iota // high-frequency historical metrics
	PriorityHealth                     // health state
	PriorityCritical                   // critical health/events
)

// Item is a single buffered payload.
type Item struct {
	Priority Priority
	Payload  []byte
}

// RingBuffer is a fixed-capacity FIFO that drops the oldest low-priority items
// when full, protecting critical items.
type RingBuffer struct {
	mu    sync.Mutex
	items []Item
	cap   int
}

// New creates a ring buffer with the given capacity (number of items).
func New(capacity int) *RingBuffer {
	if capacity < 1 {
		capacity = 1
	}
	return &RingBuffer{cap: capacity}
}

// Push adds an item, evicting the oldest lowest-priority item if at capacity.
func (r *RingBuffer) Push(it Item) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.items) < r.cap {
		r.items = append(r.items, it)
		return
	}
	// At capacity: find the oldest item with the lowest priority to evict.
	evict := -1
	lowest := PriorityCritical + 1
	for i, existing := range r.items {
		if existing.Priority < lowest {
			lowest = existing.Priority
			evict = i
		}
	}
	// Never drop a more-important item to make room for a less-important one.
	if evict >= 0 && r.items[evict].Priority <= it.Priority {
		r.items = append(r.items[:evict], r.items[evict+1:]...)
		r.items = append(r.items, it)
	}
	// Otherwise the new low-priority item is simply dropped.
}

// Drain returns all buffered items and clears the buffer.
func (r *RingBuffer) Drain() []Item {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.items
	r.items = nil
	return out
}

// Len returns the number of buffered items.
func (r *RingBuffer) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.items)
}
