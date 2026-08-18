package hash

import (
	"errors"
	"math/bits"
)

// Precision bounds. A precision of p means the sketch keeps 2^p registers, so
// the lower bound keeps the estimator meaningful and the upper bound keeps a
// single sketch under half a megabyte.
const (
	MinPrecision uint = 4
	MaxPrecision uint = 18
)

// ErrPrecisionRange is returned when a precision falls outside
// [MinPrecision, MaxPrecision].
var ErrPrecisionRange = errors.New("hash: precision out of range")

// PrecisionError carries the offending precision alongside ErrPrecisionRange
// so that callers can report it without re-deriving it.
type PrecisionError struct {
	Precision uint
}

// Error implements error.
func (e *PrecisionError) Error() string {
	return "hash: precision " + itoa(e.Precision) + " out of range [4,18]"
}

// Unwrap lets errors.Is(err, ErrPrecisionRange) succeed.
func (e *PrecisionError) Unwrap() error { return ErrPrecisionRange }

// itoa formats small unsigned integers without pulling in strconv.
func itoa(v uint) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

// ValidatePrecision reports whether p is a usable precision.
func ValidatePrecision(p uint) error {
	if p < MinPrecision || p > MaxPrecision {
		return &PrecisionError{Precision: p}
	}
	return nil
}

// RegisterCount returns the number of registers implied by precision p, that
// is 2^p.
func RegisterCount(p uint) (uint32, error) {
	if err := ValidatePrecision(p); err != nil {
		return 0, err
	}
	return uint32(1) << p, nil
}

// MaxRho returns the largest run length Split can produce at precision p.
//
// Split consumes p bits for the index and inspects the remaining 64-p bits.
// When all of those bits are zero the run length saturates at 64-p+1.
func MaxRho(p uint) (uint8, error) {
	if err := ValidatePrecision(p); err != nil {
		return 0, err
	}
	return uint8(64-p) + 1, nil
}

// Split decomposes a digest into the pair a HyperLogLog register update needs.
//
// The index is taken from the p most significant bits. The run length rho is
// the one-based position of the leftmost set bit among the remaining 64-p
// bits; when every one of those bits is zero rho saturates at 64-p+1.
//
// Both results are derived from disjoint bit ranges of h, which is what makes
// the index and the run length independent.
func Split(h uint64, p uint) (index uint32, rho uint8, err error) {
	if err := ValidatePrecision(p); err != nil {
		return 0, 0, err
	}
	index = uint32(h >> (64 - p))
	rho = rhoOf(h, p)
	return index, rho, nil
}

// Index returns just the register index of h at precision p.
func Index(h uint64, p uint) (uint32, error) {
	if err := ValidatePrecision(p); err != nil {
		return 0, err
	}
	return uint32(h >> (64 - p)), nil
}

// Rho returns just the run length of h at precision p.
func Rho(h uint64, p uint) (uint8, error) {
	if err := ValidatePrecision(p); err != nil {
		return 0, err
	}
	return rhoOf(h, p), nil
}

// rhoOf assumes p has already been validated.
//
// Shifting the digest left by p discards the index bits and moves the bits of
// interest to the top of the word, so bits.LeadingZeros64 counts exactly the
// zeros that precede the leftmost set bit of the suffix. The p vacated low
// bits are zero, so a zero suffix is the only way to get w == 0.
func rhoOf(h uint64, p uint) uint8 {
	w := h << p
	if w == 0 {
		return uint8(64-p) + 1
	}
	return uint8(bits.LeadingZeros64(w)) + 1
}

// Encode packs an index and a run length into a single 32-bit word, used by
// the sparse representation of a sketch.
//
// The layout is index in the high bits and rho in the low 8 bits. Because the
// index occupies the high bits, the natural ordering of the encoded words
// groups entries by register, which keeps a sorted sparse list scannable.
func Encode(index uint32, rho uint8) uint32 {
	return index<<8 | uint32(rho)
}

// Decode is the inverse of Encode.
func Decode(word uint32) (index uint32, rho uint8) {
	return word >> 8, uint8(word & 0xff)
}

// EncodableIndex reports whether Encode can represent index without loss.
// Encode shifts the index left by 8, so precisions above 24 would overflow;
// MaxPrecision keeps well clear of that, and this helper documents the bound.
func EncodableIndex(index uint32) bool {
	return index <= (1<<24)-1
}
