package cache

import "testing"

func TestRingBufferEvictsOldestLowPriority(t *testing.T) {
	r := New(2)
	r.Push(Item{Priority: PriorityHistorical, Payload: []byte("h1")})
	r.Push(Item{Priority: PriorityHistorical, Payload: []byte("h2")})
	// Buffer full; a critical item should evict an oldest historical one.
	r.Push(Item{Priority: PriorityCritical, Payload: []byte("crit")})

	items := r.Drain()
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	var sawCritical bool
	for _, it := range items {
		if it.Priority == PriorityCritical {
			sawCritical = true
		}
	}
	if !sawCritical {
		t.Fatalf("critical item was not retained")
	}
}

func TestRingBufferProtectsCritical(t *testing.T) {
	r := New(1)
	r.Push(Item{Priority: PriorityCritical, Payload: []byte("crit")})
	// A low-priority item must NOT evict the critical one.
	r.Push(Item{Priority: PriorityHistorical, Payload: []byte("hist")})
	items := r.Drain()
	if len(items) != 1 || items[0].Priority != PriorityCritical {
		t.Fatalf("critical item should be protected, got %+v", items)
	}
}
