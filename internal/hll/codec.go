package hll

import (
	"encoding/binary"
	"errors"
	"sort"

	"hyperloglog/internal/hash"
)

// Wire format constants.
//
// The header is fixed width so that a reader can validate a blob before
// allocating anything sized from its contents.
const (
	magicLen    = 4
	headerLen   = magicLen + 4
	wireVersion = 1

	modeDense  = 0
	modeSparse = 1

	sparseEntryLen = 5 // 4 bytes of index, 1 byte of run length
)

// magic identifies a serialised sketch.
var magic = [magicLen]byte{'H', 'L', 'L', 'S'}

// Errors returned while decoding.
var (
	// ErrBadMagic is returned when a blob does not start with the sketch
	// magic bytes.
	ErrBadMagic = errors.New("hll: bad magic")
	// ErrBadVersion is returned for a wire version this build cannot read.
	ErrBadVersion = errors.New("hll: unsupported wire version")
	// ErrBadMode is returned when the representation byte is neither dense
	// nor sparse.
	ErrBadMode = errors.New("hll: unknown representation")
	// ErrTruncated is returned when a blob ends before its declared
	// contents do.
	ErrTruncated = errors.New("hll: truncated payload")
	// ErrTrailingBytes is returned when a blob has bytes left over after
	// its declared contents.
	ErrTrailingBytes = errors.New("hll: trailing bytes after payload")
	// ErrIndexOutOfRange is returned when a sparse entry names a register
	// the declared precision does not have.
	ErrIndexOutOfRange = errors.New("hll: register index out of range")
	// ErrRhoOutOfRange is returned when a register value exceeds what the
	// declared precision can produce.
	ErrRhoOutOfRange = errors.New("hll: register value out of range")
)

// MarshalBinary serialises the sketch.
//
// The representation is preserved: a sparse sketch encodes only its occupied
// registers, so a blob for a small cardinality stays small. Sparse entries are
// written in ascending index order, which makes the encoding canonical - two
// sketches with equal registers produce byte-identical blobs.
func (h *HLL) MarshalBinary() ([]byte, error) {
	if h.sparse != nil {
		return h.marshalSparse(), nil
	}
	return h.marshalDense(), nil
}

func (h *HLL) marshalDense() []byte {
	out := make([]byte, 0, headerLen+len(h.dense))
	out = append(out, magic[:]...)
	out = append(out, wireVersion, byte(h.p), modeDense, 0)
	out = append(out, h.dense...)
	return out
}

func (h *HLL) marshalSparse() []byte {
	indexes := make([]uint32, 0, len(h.sparse))
	for index := range h.sparse {
		indexes = append(indexes, index)
	}
	sort.Slice(indexes, func(i, j int) bool { return indexes[i] < indexes[j] })

	out := make([]byte, 0, headerLen+4+len(indexes)*sparseEntryLen)
	out = append(out, magic[:]...)
	out = append(out, wireVersion, byte(h.p), modeSparse, 0)

	var count [4]byte
	binary.BigEndian.PutUint32(count[:], uint32(len(indexes)))
	out = append(out, count[:]...)

	var entry [4]byte
	for _, index := range indexes {
		binary.BigEndian.PutUint32(entry[:], index)
		out = append(out, entry[:]...)
		out = append(out, h.sparse[index])
	}
	return out
}

// UnmarshalBinary replaces the receiver's contents with the decoded sketch.
//
// Nothing is written to the receiver until the whole blob has validated, so a
// failed decode leaves the previous contents intact.
func (h *HLL) UnmarshalBinary(data []byte) error {
	if len(data) < headerLen {
		return ErrTruncated
	}
	for i := 0; i < magicLen; i++ {
		if data[i] != magic[i] {
			return ErrBadMagic
		}
	}
	if data[magicLen] != wireVersion {
		return ErrBadVersion
	}

	p := uint(data[magicLen+1])
	m, err := hash.RegisterCount(p)
	if err != nil {
		return err
	}
	maxRho, err := hash.MaxRho(p)
	if err != nil {
		return err
	}

	body := data[headerLen:]
	switch data[magicLen+2] {
	case modeDense:
		return h.unmarshalDense(p, m, maxRho, body)
	case modeSparse:
		return h.unmarshalSparse(p, m, maxRho, body)
	default:
		return ErrBadMode
	}
}

func (h *HLL) unmarshalDense(p uint, m uint32, maxRho uint8, body []byte) error {
	if uint32(len(body)) < m {
		return ErrTruncated
	}
	if uint32(len(body)) > m {
		return ErrTrailingBytes
	}
	for _, v := range body {
		if v > maxRho {
			return ErrRhoOutOfRange
		}
	}

	dense := make([]uint8, m)
	copy(dense, body)

	h.p = p
	h.m = m
	h.maxSparse = sparseLimit(m)
	h.dense = dense
	h.sparse = nil
	return nil
}

func (h *HLL) unmarshalSparse(p uint, m uint32, maxRho uint8, body []byte) error {
	if len(body) < 4 {
		return ErrTruncated
	}
	count := binary.BigEndian.Uint32(body[:4])
	entries := body[4:]

	// Compare in a wide type so that a hostile count cannot wrap the
	// multiplication and make a truncated blob look complete.
	if uint64(len(entries)) < uint64(count)*sparseEntryLen {
		return ErrTruncated
	}
	if uint64(len(entries)) > uint64(count)*sparseEntryLen {
		return ErrTrailingBytes
	}

	sparse := make(map[uint32]uint8, count)
	for i := uint32(0); i < count; i++ {
		off := int(i) * sparseEntryLen
		index := binary.BigEndian.Uint32(entries[off : off+4])
		rho := entries[off+4]
		if index >= m {
			return ErrIndexOutOfRange
		}
		if rho == 0 || rho > maxRho {
			return ErrRhoOutOfRange
		}
		if cur, ok := sparse[index]; !ok || rho > cur {
			sparse[index] = rho
		}
	}

	h.p = p
	h.m = m
	h.maxSparse = sparseLimit(m)
	h.dense = nil
	h.sparse = sparse
	if len(h.sparse) > h.maxSparse {
		h.promote()
	}
	return nil
}

// Unmarshal decodes a blob into a freshly allocated sketch.
func Unmarshal(data []byte) (*HLL, error) {
	h := &HLL{}
	if err := h.UnmarshalBinary(data); err != nil {
		return nil, err
	}
	return h, nil
}
