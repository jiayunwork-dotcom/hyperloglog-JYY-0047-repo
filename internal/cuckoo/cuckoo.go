// Package cuckoo implements a Cuckoo filter - a space-efficient probabilistic
// data structure for approximate set membership, similar to a Bloom filter but
// with support for deletion. It uses cuckoo hashing to store fingerprints.
package cuckoo

import (
	"encoding/binary"
	"errors"
	"hash/fnv"
	"math/rand"
)

const (
	bucketSize  = 4  // entries per bucket
	maxKicks    = 500
	fingerprintBits = 16
)

// ErrFull is returned when the filter is at capacity and cannot insert.
var ErrFull = errors.New("cuckoo: filter is full")

// Filter is a Cuckoo filter.
type Filter struct {
	buckets []bucket
	count   uint64
	numBuckets uint
	rng     *rand.Rand
}

type bucket [bucketSize]uint16

// New creates a cuckoo filter with the given capacity (approximate).
func New(capacity int) *Filter {
	numBuckets := nextPowerOf2(uint(capacity) / bucketSize)
	if numBuckets == 0 {
		numBuckets = 1
	}
	return &Filter{
		buckets:    make([]bucket, numBuckets),
		numBuckets: numBuckets,
		rng:        rand.New(rand.NewSource(42)),
	}
}

// Insert adds an element. Returns ErrFull if the filter is at capacity.
func (f *Filter) Insert(data []byte) error {
	fp := fingerprint(data)
	i1 := f.index(data)
	i2 := f.altIndex(i1, fp)

	if f.buckets[i1].insert(fp) || f.buckets[i2].insert(fp) {
		f.count++
		return nil
	}

	// Kick existing entries.
	idx := i1
	if f.rng.Intn(2) == 0 {
		idx = i2
	}
	for kick := 0; kick < maxKicks; kick++ {
		slot := f.rng.Intn(bucketSize)
		fp, f.buckets[idx][slot] = f.buckets[idx][slot], fp
		idx = f.altIndex(idx, fp)
		if f.buckets[idx].insert(fp) {
			f.count++
			return nil
		}
	}
	return ErrFull
}

// Contains returns true if the element might be in the filter.
func (f *Filter) Contains(data []byte) bool {
	fp := fingerprint(data)
	i1 := f.index(data)
	i2 := f.altIndex(i1, fp)
	return f.buckets[i1].contains(fp) || f.buckets[i2].contains(fp)
}

// Delete removes an element. Returns false if not found.
func (f *Filter) Delete(data []byte) bool {
	fp := fingerprint(data)
	i1 := f.index(data)
	i2 := f.altIndex(i1, fp)
	if f.buckets[i1].remove(fp) {
		f.count--
		return true
	}
	if f.buckets[i2].remove(fp) {
		f.count--
		return true
	}
	return false
}

// Count returns the number of items inserted.
func (f *Filter) Count() uint64 { return f.count }

// LoadFactor returns the fraction of slots occupied.
func (f *Filter) LoadFactor() float64 {
	totalSlots := uint64(f.numBuckets) * bucketSize
	return float64(f.count) / float64(totalSlots)
}

// Reset clears the filter.
func (f *Filter) Reset() {
	for i := range f.buckets {
		f.buckets[i] = bucket{}
	}
	f.count = 0
}

func (f *Filter) index(data []byte) uint {
	h := fnv.New64a()
	_, _ = h.Write(data)
	return uint(h.Sum64()) % f.numBuckets
}

func (f *Filter) altIndex(i uint, fp uint16) uint {
	h := fnv.New64a()
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], fp)
	_, _ = h.Write(buf[:])
	return (i ^ uint(h.Sum64())) % f.numBuckets
}

func fingerprint(data []byte) uint16 {
	h := fnv.New32a()
	_, _ = h.Write(data)
	fp := uint16(h.Sum32() % (1 << fingerprintBits))
	if fp == 0 {
		fp = 1
	}
	return fp
}

func (b *bucket) insert(fp uint16) bool {
	for i := range b {
		if b[i] == 0 {
			b[i] = fp
			return true
		}
	}
	return false
}

func (b *bucket) contains(fp uint16) bool {
	for _, v := range b {
		if v == fp {
			return true
		}
	}
	return false
}

func (b *bucket) remove(fp uint16) bool {
	for i, v := range b {
		if v == fp {
			b[i] = 0
			return true
		}
	}
	return false
}

func nextPowerOf2(v uint) uint {
	v--
	v |= v >> 1
	v |= v >> 2
	v |= v >> 4
	v |= v >> 8
	v |= v >> 16
	v++
	if v == 0 {
		v = 1
	}
	return v
}
