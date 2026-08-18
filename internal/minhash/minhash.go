// Package minhash implements MinHash for estimating Jaccard similarity between
// sets. Each set is represented by a signature of k minimum hash values;
// comparing two signatures gives an unbiased estimate of the Jaccard index.
package minhash

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"sort"
	"sync"
)

// Signature is a MinHash signature of fixed size.
type Signature struct {
	mu     sync.Mutex
	values []uint64
	k      int
}

// New creates a signature with k hash functions.
func New(k int) *Signature {
	if k <= 0 {
		k = 128
	}
	values := make([]uint64, k)
	for i := range values {
		values[i] = math.MaxUint64
	}
	return &Signature{values: values, k: k}
}

// Add incorporates an element into the signature.
func (s *Signature) Add(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < s.k; i++ {
		h := hashWithSeed(data, uint64(i))
		if h < s.values[i] {
			s.values[i] = h
		}
	}
}

// Similarity estimates the Jaccard similarity between two signatures.
func Similarity(a, b *Signature) float64 {
	if a.k != b.k {
		return 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	matches := 0
	for i := 0; i < a.k; i++ {
		if a.values[i] == b.values[i] {
			matches++
		}
	}
	return float64(matches) / float64(a.k)
}

// Merge combines two signatures into one (union operation).
func Merge(a, b *Signature) *Signature {
	if a.k != b.k {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	result := New(a.k)
	for i := 0; i < a.k; i++ {
		if a.values[i] < b.values[i] {
			result.values[i] = a.values[i]
		} else {
			result.values[i] = b.values[i]
		}
	}
	return result
}

// Values returns a copy of the signature values.
func (s *Signature) Values() []uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]uint64, s.k)
	copy(out, s.values)
	return out
}

// Size returns the number of hash functions (k).
func (s *Signature) Size() int { return s.k }

// Reset clears the signature.
func (s *Signature) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.values {
		s.values[i] = math.MaxUint64
	}
}

// IsEmpty reports whether no elements have been added.
func (s *Signature) IsEmpty() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.values {
		if v != math.MaxUint64 {
			return false
		}
	}
	return true
}

// CardinalityEstimate gives a rough cardinality estimate using the k-th minimum.
func (s *Signature) CardinalityEstimate() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	sorted := make([]uint64, s.k)
	copy(sorted, s.values)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	kth := sorted[s.k-1]
	if kth == 0 || kth == math.MaxUint64 {
		return 0
	}
	return float64(s.k-1) / (float64(kth) / float64(math.MaxUint64))
}

func hashWithSeed(data []byte, seed uint64) uint64 {
	h := fnv.New64a()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], seed)
	_, _ = h.Write(buf[:])
	_, _ = h.Write(data)
	return h.Sum64()
}
