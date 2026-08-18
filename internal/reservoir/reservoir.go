// Package reservoir implements reservoir sampling - a family of algorithms for
// choosing a simple random sample of k items from a stream of unknown length.
// This is useful alongside HLL for maintaining a sample of representative items
// while counting distinct ones.
package reservoir

import (
	"math/rand"
	"sort"
	"sync"
)

// Sample maintains a fixed-size random sample from a stream.
type Sample struct {
	mu   sync.Mutex
	k    int
	items []string
	seen  int64
	rng   *rand.Rand
}

// New creates a reservoir of capacity k.
func New(k int) *Sample {
	if k <= 0 {
		k = 100
	}
	return &Sample{
		k:     k,
		items: make([]string, 0, k),
		rng:   rand.New(rand.NewSource(42)),
	}
}

// NewWithSeed creates a sample with a custom random seed.
func NewWithSeed(k int, seed int64) *Sample {
	s := New(k)
	s.rng = rand.New(rand.NewSource(seed))
	return s
}

// Add observes a new item from the stream.
func (s *Sample) Add(item string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen++
	if len(s.items) < s.k {
		s.items = append(s.items, item)
		return
	}
	// Replace with probability k/seen.
	j := s.rng.Int63n(s.seen)
	if j < int64(s.k) {
		s.items[j] = item
	}
}

// Items returns the current sample.
func (s *Sample) Items() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.items))
	copy(out, s.items)
	return out
}

// Len returns how many items are in the sample.
func (s *Sample) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

// Seen returns total items observed.
func (s *Sample) Seen() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen
}

// Capacity returns the max sample size.
func (s *Sample) Capacity() int { return s.k }

// Reset clears the sample.
func (s *Sample) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = s.items[:0]
	s.seen = 0
}

// Contains reports whether item is currently in the sample.
func (s *Sample) Contains(item string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.items {
		if v == item {
			return true
		}
	}
	return false
}

// Sorted returns the sample in sorted order.
func (s *Sample) Sorted() []string {
	items := s.Items()
	sort.Strings(items)
	return items
}

// SamplingRate returns k / seen.
func (s *Sample) SamplingRate() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.seen == 0 {
		return 0
	}
	return float64(s.k) / float64(s.seen)
}
