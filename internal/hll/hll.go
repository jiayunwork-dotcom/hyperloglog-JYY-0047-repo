// Package hll implements the HyperLogLog cardinality estimator.
//
// A sketch keeps m = 2^p one-byte registers. Every observed element is hashed
// to 64 bits; the leading p bits select a register and the position of the
// leftmost set bit in the remaining suffix is stored in that register if it
// exceeds the value already there. The harmonic mean of the register values,
// scaled by a bias constant, estimates how many distinct elements were seen.
//
// Two representations are used. Below a threshold the sketch stores only the
// registers that have actually been touched, in a map keyed by register index;
// this keeps memory proportional to the observed cardinality and makes small
// counts essentially exact. Once too many registers are occupied the sketch
// promotes itself to the flat dense array, which from then on has a fixed
// footprint of m bytes.
package hll

import (
	"bufio"
	"errors"
	"io"
	"strings"

	"hyperloglog/internal/hash"
)

// DefaultPrecision is the precision used by NewDefault. At p=14 a sketch
// occupies 16 KiB dense and has a relative standard error near 0.81%.
const DefaultPrecision uint = 14

// MinPrecision and MaxPrecision mirror the bounds enforced by the hash
// package, re-exported so callers do not have to import it.
const (
	MinPrecision = hash.MinPrecision
	MaxPrecision = hash.MaxPrecision
)

// Sentinel errors returned by this package.
var (
	// ErrEmptyItem is returned when an element with no bytes is offered.
	ErrEmptyItem = errors.New("hll: empty item")
	// ErrNilSketch is returned when a nil sketch is passed to Merge.
	ErrNilSketch = errors.New("hll: nil sketch")
	// ErrPrecisionMismatch is returned when merging sketches whose
	// precisions differ; their registers are not comparable.
	ErrPrecisionMismatch = errors.New("hll: precision mismatch")
	// ErrPrecisionRange is returned for an out-of-range precision.
	ErrPrecisionRange = hash.ErrPrecisionRange
)

// maxLineBytes bounds a single line read by AddLines.
const maxLineBytes = 1 << 20

// HLL is a HyperLogLog sketch. It is not safe for concurrent use.
//
// Exactly one of dense and sparse is non-nil at any time: a sketch is either
// in the sparse representation or the dense one, never both and never neither.
type HLL struct {
	p         uint
	m         uint32
	dense     []uint8
	sparse    map[uint32]uint8
	maxSparse int
}

// New returns an empty sketch with the requested precision.
//
// The sketch starts in the sparse representation.
func New(p uint) (*HLL, error) {
	m, err := hash.RegisterCount(p)
	if err != nil {
		return nil, err
	}
	return &HLL{
		p:         p,
		m:         m,
		sparse:    make(map[uint32]uint8),
		maxSparse: sparseLimit(m),
	}, nil
}

// NewDefault returns an empty sketch at DefaultPrecision. DefaultPrecision is
// always in range, so no error is possible.
func NewDefault() *HLL {
	h, err := New(DefaultPrecision)
	if err != nil {
		panic("hll: DefaultPrecision must be valid: " + err.Error())
	}
	return h
}

// sparseLimit picks the number of occupied registers at which the sparse
// representation stops paying for itself.
//
// A sparse entry costs roughly a map slot, several times more than the single
// byte a dense register costs, so the crossover sits well below m. A floor
// keeps tiny sketches from promoting on their first few elements, and the cap
// at m-1 keeps the limit reachable: a sparse map can hold at most m distinct
// register indexes, so a limit of m or more would never trigger promotion.
func sparseLimit(m uint32) int {
	limit := int(m / 4)
	if limit < 16 {
		limit = 16
	}
	if ceiling := int(m) - 1; limit > ceiling {
		limit = ceiling
	}
	return limit
}

// Precision returns the precision the sketch was built with.
func (h *HLL) Precision() uint { return h.p }

// RegisterCount returns m, the number of registers.
func (h *HLL) RegisterCount() uint32 { return h.m }

// IsSparse reports whether the sketch is still in the sparse representation.
func (h *HLL) IsSparse() bool { return h.sparse != nil }

// SparseSize returns the number of occupied registers while sparse, and 0
// once the sketch has been promoted to dense.
func (h *HLL) SparseSize() int {
	if h.sparse == nil {
		return 0
	}
	return len(h.sparse)
}

// SparseLimit returns the occupancy at which promotion happens.
func (h *HLL) SparseLimit() int { return h.maxSparse }

// Add folds an already hashed element into the sketch.
//
// Callers that hold raw elements should prefer AddBytes or AddString; Add is
// for callers that maintain their own digests, and for replaying a stream of
// digests captured earlier.
func (h *HLL) Add(hashed uint64) error {
	index, rho, err := hash.Split(hashed, h.p)
	if err != nil {
		return err
	}
	h.update(index, rho)
	return nil
}

// AddBytes hashes item and folds it in. An item with no bytes is rejected:
// distinct-count inputs are identifiers, and an empty identifier is almost
// always a truncated read rather than a real element.
func (h *HLL) AddBytes(item []byte) error {
	if len(item) == 0 {
		return ErrEmptyItem
	}
	return h.Add(hash.Hash64(item))
}

// AddString is the string flavour of AddBytes.
func (h *HLL) AddString(item string) error {
	if item == "" {
		return ErrEmptyItem
	}
	return h.Add(hash.Hash64String(item))
}

// AddLines reads one element per line from r and folds each into the sketch.
//
// Surrounding whitespace is trimmed and lines that are empty after trimming
// are skipped rather than rejected, so a trailing newline or a blank
// separator line does not fail the whole stream. The count of elements
// actually folded in is returned.
func (h *HLL) AddLines(r io.Reader) (int, error) {
	if r == nil {
		return 0, ErrNilSketch
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	added := 0
	for sc.Scan() {
		item := strings.TrimSpace(sc.Text())
		if item == "" {
			continue
		}
		if err := h.AddString(item); err != nil {
			return added, err
		}
		added++
	}
	if err := sc.Err(); err != nil {
		return added, err
	}
	return added, nil
}

// update applies one (index, rho) observation. A register only ever moves
// upwards, which is what makes the sketch order independent and idempotent.
func (h *HLL) update(index uint32, rho uint8) {
	if h.sparse != nil {
		if cur, ok := h.sparse[index]; !ok || rho > cur {
			h.sparse[index] = rho
		}
		if len(h.sparse) > h.maxSparse {
			h.promote()
		}
		return
	}
	if rho > h.dense[index] {
		h.dense[index] = rho
	}
}

// promote converts the sparse representation into the dense one. It is a no-op
// on a sketch that is already dense.
func (h *HLL) promote() {
	if h.sparse == nil {
		return
	}
	dense := make([]uint8, h.m)
	for index, rho := range h.sparse {
		if rho > dense[index] {
			dense[index] = rho
		}
	}
	h.dense = dense
	h.sparse = nil
}

// Densify forces the sketch into the dense representation.
//
// Estimates are unchanged by this call; it exists so that callers who know a
// sketch is about to receive many elements can pay the conversion once.
func (h *HLL) Densify() {
	h.promote()
}

// Registers returns a dense snapshot of the register array.
//
// The result is always a fresh slice of length m, whichever representation the
// sketch is in, and the sketch itself is left untouched. Callers may modify
// the returned slice freely.
func (h *HLL) Registers() []uint8 {
	out := make([]uint8, h.m)
	if h.sparse != nil {
		for index, rho := range h.sparse {
			if rho > out[index] {
				out[index] = rho
			}
		}
		return out
	}
	copy(out, h.dense)
	return out
}

// Merge folds other into h so that h estimates the cardinality of the union.
//
// Registers combine by taking the larger value, which is exactly what makes
// HyperLogLog mergeable without loss: the union sketch is identical to the one
// that would have resulted from feeding both element streams into a single
// sketch. other is not modified.
func (h *HLL) Merge(other *HLL) error {
	if other == nil {
		return ErrNilSketch
	}
	if other.p != h.p {
		return ErrPrecisionMismatch
	}
	if h == other {
		return nil
	}

	if h.sparse != nil && other.sparse != nil {
		for index, rho := range other.sparse {
			if cur, ok := h.sparse[index]; !ok || rho > cur {
				h.sparse[index] = rho
			}
		}
		if len(h.sparse) > h.maxSparse {
			h.promote()
		}
		return nil
	}

	h.promote()
	src := other.Registers()
	for i, rho := range src {
		if rho > h.dense[i] {
			h.dense[i] = rho
		}
	}
	return nil
}

// Union returns a new sketch holding the union of a and b, leaving both
// operands untouched.
func Union(a, b *HLL) (*HLL, error) {
	if a == nil || b == nil {
		return nil, ErrNilSketch
	}
	out := a.Clone()
	if err := out.Merge(b); err != nil {
		return nil, err
	}
	return out, nil
}

// Clone returns an independent deep copy of the sketch, in the same
// representation as the original.
func (h *HLL) Clone() *HLL {
	out := &HLL{
		p:         h.p,
		m:         h.m,
		maxSparse: h.maxSparse,
	}
	if h.sparse != nil {
		out.sparse = make(map[uint32]uint8, len(h.sparse))
		for index, rho := range h.sparse {
			out.sparse[index] = rho
		}
		return out
	}
	out.dense = make([]uint8, len(h.dense))
	copy(out.dense, h.dense)
	return out
}

// Reset empties the sketch and returns it to the sparse representation,
// keeping its precision.
func (h *HLL) Reset() {
	h.dense = nil
	h.sparse = make(map[uint32]uint8)
}

// Equal reports whether two sketches would produce the same estimate for
// every possible future element stream, that is whether they have the same
// precision and the same register values. The representation is ignored.
func (h *HLL) Equal(other *HLL) bool {
	if other == nil {
		return false
	}
	if h.p != other.p {
		return false
	}
	left := h.Registers()
	right := other.Registers()
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
