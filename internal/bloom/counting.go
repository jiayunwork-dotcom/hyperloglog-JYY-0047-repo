package bloom

import (
	"sync"
)

// CountingFilter is a Bloom filter that supports deletion by using counters
// instead of single bits.
type CountingFilter struct {
	mu       sync.RWMutex
	counters []uint8
	m        uint64
	k        uint64
	n        uint64
}

// NewCounting creates a counting Bloom filter.
func NewCounting(expectedN int, fpRate float64) *CountingFilter {
	f := New(expectedN, fpRate)
	return &CountingFilter{
		counters: make([]uint8, f.m),
		m:        f.m,
		k:        f.k,
	}
}

// Add inserts an element.
func (cf *CountingFilter) Add(data []byte) {
	h1, h2 := hashes(data)
	cf.mu.Lock()
	defer cf.mu.Unlock()
	for i := uint64(0); i < cf.k; i++ {
		pos := (h1 + i*h2) % cf.m
		if cf.counters[pos] < 255 {
			cf.counters[pos]++
		}
	}
	cf.n++
}

// Contains returns true if the element might be in the set.
func (cf *CountingFilter) Contains(data []byte) bool {
	h1, h2 := hashes(data)
	cf.mu.RLock()
	defer cf.mu.RUnlock()
	for i := uint64(0); i < cf.k; i++ {
		pos := (h1 + i*h2) % cf.m
		if cf.counters[pos] == 0 {
			return false
		}
	}
	return true
}

// Remove removes an element. Returns false if not present.
func (cf *CountingFilter) Remove(data []byte) bool {
	if !cf.Contains(data) {
		return false
	}
	h1, h2 := hashes(data)
	cf.mu.Lock()
	defer cf.mu.Unlock()
	for i := uint64(0); i < cf.k; i++ {
		pos := (h1 + i*h2) % cf.m
		if cf.counters[pos] > 0 {
			cf.counters[pos]--
		}
	}
	cf.n--
	return true
}

// Count returns items added.
func (cf *CountingFilter) Count() uint64 {
	cf.mu.RLock()
	defer cf.mu.RUnlock()
	return cf.n
}

// Reset clears the filter.
func (cf *CountingFilter) Reset() {
	cf.mu.Lock()
	defer cf.mu.Unlock()
	for i := range cf.counters {
		cf.counters[i] = 0
	}
	cf.n = 0
}
