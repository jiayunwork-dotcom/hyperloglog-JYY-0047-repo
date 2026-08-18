// Package bloom implements a standard Bloom filter for approximate set
// membership queries. While HyperLogLog estimates cardinality, the Bloom
// filter answers "is this element possibly in the set?" with configurable
// false-positive rates.
package bloom

import (
	"encoding/binary"
	"hash/fnv"
	"io"
	"math"
	"sync"
)

// Filter is a concurrent-safe Bloom filter.
type Filter struct {
	mu   sync.RWMutex
	bits []uint64
	m    uint64
	k    uint64
	n    uint64
}

// New creates a filter sized for expectedN items at fpRate.
func New(expectedN int, fpRate float64) *Filter {
	if expectedN <= 0 {
		expectedN = 1
	}
	if fpRate <= 0 || fpRate >= 1 {
		fpRate = 0.01
	}
	m := uint64(math.Ceil(-float64(expectedN) * math.Log(fpRate) / (math.Ln2 * math.Ln2)))
	if m == 0 {
		m = 1
	}
	k := uint64(math.Round(float64(m) / float64(expectedN) * math.Ln2))
	if k == 0 {
		k = 1
	}
	return &Filter{bits: make([]uint64, (m+63)/64), m: m, k: k}
}

// Add inserts an element.
func (f *Filter) Add(data []byte) {
	h1, h2 := hashes(data)
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := uint64(0); i < f.k; i++ {
		pos := (h1 + i*h2) % f.m
		f.bits[pos/64] |= 1 << (pos % 64)
	}
	f.n++
}

// Contains returns true if the element might be in the set.
func (f *Filter) Contains(data []byte) bool {
	h1, h2 := hashes(data)
	f.mu.RLock()
	defer f.mu.RUnlock()
	for i := uint64(0); i < f.k; i++ {
		pos := (h1 + i*h2) % f.m
		if f.bits[pos/64]&(1<<(pos%64)) == 0 {
			return false
		}
	}
	return true
}

// Count returns items added.
func (f *Filter) Count() uint64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.n
}

// FillRatio returns the fraction of bits set.
func (f *Filter) FillRatio() float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var set uint64
	for _, w := range f.bits {
		set += popcount(w)
	}
	return float64(set) / float64(f.m)
}

// EstimateFP returns the estimated current false-positive rate.
func (f *Filter) EstimateFP() float64 {
	f.mu.RLock()
	n := f.n
	f.mu.RUnlock()
	return math.Pow(1-math.Exp(-float64(f.k)*float64(n)/float64(f.m)), float64(f.k))
}

// Reset clears the filter.
func (f *Filter) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.bits {
		f.bits[i] = 0
	}
	f.n = 0
}

// WriteTo serializes the filter.
func (f *Filter) WriteTo(w io.Writer) (int64, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var hdr [24]byte
	binary.BigEndian.PutUint64(hdr[0:8], f.m)
	binary.BigEndian.PutUint64(hdr[8:16], f.k)
	binary.BigEndian.PutUint64(hdr[16:24], f.n)
	n, err := w.Write(hdr[:])
	if err != nil {
		return int64(n), err
	}
	total := int64(n)
	buf := make([]byte, 8)
	for _, word := range f.bits {
		binary.BigEndian.PutUint64(buf, word)
		nn, err := w.Write(buf)
		total += int64(nn)
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// ReadFrom deserializes a filter.
func (f *Filter) ReadFrom(r io.Reader) (int64, error) {
	var hdr [24]byte
	n, err := io.ReadFull(r, hdr[:])
	if err != nil {
		return int64(n), err
	}
	total := int64(n)
	m := binary.BigEndian.Uint64(hdr[0:8])
	k := binary.BigEndian.Uint64(hdr[8:16])
	count := binary.BigEndian.Uint64(hdr[16:24])
	words := (m + 63) / 64
	bits := make([]uint64, words)
	buf := make([]byte, 8)
	for i := range bits {
		nn, err := io.ReadFull(r, buf)
		total += int64(nn)
		if err != nil {
			return total, err
		}
		bits[i] = binary.BigEndian.Uint64(buf)
	}
	f.mu.Lock()
	f.bits = bits
	f.m = m
	f.k = k
	f.n = count
	f.mu.Unlock()
	return total, nil
}

func hashes(data []byte) (uint64, uint64) {
	h1 := fnv.New64a()
	_, _ = h1.Write(data)
	v1 := h1.Sum64()
	h2 := fnv.New64()
	_, _ = h2.Write(data)
	v2 := h2.Sum64()
	if v2 == 0 {
		v2 = 1
	}
	return v1, v2
}

func popcount(x uint64) uint64 {
	x = x - ((x >> 1) & 0x5555555555555555)
	x = (x & 0x3333333333333333) + ((x >> 2) & 0x3333333333333333)
	x = (x + (x >> 4)) & 0x0f0f0f0f0f0f0f0f
	return (x * 0x0101010101010101) >> 56
}
