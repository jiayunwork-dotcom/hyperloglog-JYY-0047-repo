// Package hash provides the 64-bit hashing primitives shared by the
// cardinality and frequency sketches in this module.
//
// The base function is FNV-1a over 64 bits followed by a finalisation
// (avalanche) step. FNV-1a alone has poor diffusion in its high bits, which
// matters a great deal for HyperLogLog because HyperLogLog derives the
// register index from the leading bits of the digest. The finalisation step
// spreads every input bit across the whole 64-bit word so that both the
// register index and the run-length estimate behave like uniform random
// draws.
package hash

import (
	"errors"
	"fmt"
	"math/bits"
)

// FNV-1a 64-bit parameters.
const (
	offsetBasis64 = uint64(14695981039346656037)
	prime64       = uint64(1099511628211)
)

// Mixing constants taken from the well known 64-bit avalanche function used
// by MurmurHash3's finaliser.
const (
	mixC1 = uint64(0xff51afd7ed558ccd)
	mixC2 = uint64(0xc4ceb9fe1a85ec53)
)

// Golden-ratio derived odd constant, used to derive independent seeds.
const goldenGamma = uint64(0x9e3779b97f4a7c15)

// ErrNilDigest is returned when a method is called on a nil *Digest.
var ErrNilDigest = errors.New("hash: nil digest")

// Hash64 returns the 64-bit digest of data.
//
// The empty slice and a nil slice hash to the same value: the finalised
// offset basis. Callers that need to reject empty input must check the input
// themselves; Hash64 is a total function.
func Hash64(data []byte) uint64 {
	return Mix64(fold(offsetBasis64, data))
}

// Hash64String is the string flavour of Hash64. It allocates nothing.
func Hash64String(s string) uint64 {
	h := offsetBasis64
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return Mix64(h)
}

// Seeded returns a digest of data that is independent of Hash64(data) for
// every distinct seed. It is used to obtain the several pairwise independent
// hash functions a Count-Min sketch needs.
func Seeded(data []byte, seed uint64) uint64 {
	return Mix64(fold(offsetBasis64^seed, data) ^ seed)
}

// DeriveSeed maps a small row ordinal to a well spread 64-bit seed. Seeds are
// deterministic across processes and architectures so that a serialised
// sketch stays readable.
func DeriveSeed(i int) uint64 {
	return Mix64(uint64(i+1) * goldenGamma)
}

// Mix64 is the finalisation step: a bijection on uint64 with good avalanche
// behaviour. Being a bijection matters, because it means the mixer never
// introduces collisions of its own.
//
// Zero is a fixed point of Mix64. That is harmless here because every caller
// feeds it a state seeded from the FNV offset basis, which is never zero.
func Mix64(x uint64) uint64 {
	x ^= x >> 33
	x *= mixC1
	x ^= x >> 29
	x *= mixC2
	x ^= x >> 32
	return x
}

// fold runs the FNV-1a inner loop starting from the supplied state.
func fold(state uint64, data []byte) uint64 {
	for _, b := range data {
		state ^= uint64(b)
		state *= prime64
	}
	return state
}

// Digest is the streaming form of Hash64. It lets a caller feed input in
// arbitrarily sized chunks and still obtain the digest of the concatenation.
//
// Digest implements io.Writer. Write never fails, but the signature is kept
// so that a Digest can be handed to io.Copy or fmt.Fprintf.
type Digest struct {
	state uint64
	n     int64
}

// NewDigest returns a Digest positioned at the start of a fresh stream.
func NewDigest() *Digest {
	d := &Digest{}
	d.Reset()
	return d
}

// Reset returns the digest to the state of a fresh stream.
func (d *Digest) Reset() {
	if d == nil {
		return
	}
	d.state = offsetBasis64
	d.n = 0
}

// Write folds p into the running state.
func (d *Digest) Write(p []byte) (int, error) {
	if d == nil {
		return 0, ErrNilDigest
	}
	d.state = fold(d.state, p)
	d.n += int64(len(p))
	return len(p), nil
}

// WriteString folds s into the running state without copying it.
func (d *Digest) WriteString(s string) (int, error) {
	if d == nil {
		return 0, ErrNilDigest
	}
	for i := 0; i < len(s); i++ {
		d.state ^= uint64(s[i])
		d.state *= prime64
	}
	d.n += int64(len(s))
	return len(s), nil
}

// WriteByte folds a single byte into the running state.
func (d *Digest) WriteByte(b byte) error {
	if d == nil {
		return ErrNilDigest
	}
	d.state ^= uint64(b)
	d.state *= prime64
	d.n++
	return nil
}

// Sum64 returns the digest of everything written so far. It does not consume
// or alter the state, so it may be called repeatedly.
func (d *Digest) Sum64() uint64 {
	if d == nil {
		return 0
	}
	return Mix64(d.state)
}

// Len reports how many bytes have been written since the last Reset.
func (d *Digest) Len() int64 {
	if d == nil {
		return 0
	}
	return d.n
}

// Size implements part of the hash.Hash64 shape.
func (d *Digest) Size() int { return 8 }

// BlockSize implements part of the hash.Hash64 shape.
func (d *Digest) BlockSize() int { return 1 }

// String renders the digest in the canonical lower-case hexadecimal form.
func (d *Digest) String() string {
	return fmt.Sprintf("%016x", d.Sum64())
}

// PopCount reports the number of set bits in x. It is exported because the
// sketch packages use it when reporting how well spread a digest stream is.
func PopCount(x uint64) int {
	return bits.OnesCount64(x)
}
