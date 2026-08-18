package hll

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"hyperloglog/internal/hash"
)

// fill folds n synthetic elements carrying the given tag into the sketch.
func fill(t *testing.T, h *HLL, tag string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := h.AddString(fmt.Sprintf("%s-%d", tag, i)); err != nil {
			t.Fatalf("AddString(%s-%d): %v", tag, i, err)
		}
	}
}

func TestHLLMonotonic(t *testing.T) {
	for _, p := range []uint{10, 12, 14} {
		h, err := New(p)
		if err != nil {
			t.Fatalf("New(%d): %v", p, err)
		}
		if got := h.Count(); got != 0 {
			t.Fatalf("p=%d: empty sketch Count = %d, want 0", p, got)
		}

		prev := uint64(0)
		for i := 0; i < 30000; i++ {
			if err := h.AddString(fmt.Sprintf("mono-%d-%d", p, i)); err != nil {
				t.Fatalf("p=%d: AddString: %v", p, err)
			}
			got := h.Count()
			if got < prev {
				t.Fatalf("p=%d: after %d elements Count fell from %d to %d", p, i+1, prev, got)
			}
			prev = got
		}
	}
}

func TestHLLIdempotentAdd(t *testing.T) {
	h := NewDefault()
	fill(t, h, "idem", 5000)
	baseline := h.Count()
	registers := h.Registers()

	for round := 0; round < 3; round++ {
		fill(t, h, "idem", 5000)
		if got := h.Count(); got != baseline {
			t.Fatalf("round %d: re-adding the same elements moved Count from %d to %d", round, baseline, got)
		}
	}

	after := h.Registers()
	for i := range registers {
		if registers[i] != after[i] {
			t.Fatalf("register %d moved from %d to %d on a duplicate add", i, registers[i], after[i])
		}
	}

	h2 := NewDefault()
	for i := 4999; i >= 0; i-- {
		if err := h2.AddString(fmt.Sprintf("idem-%d", i)); err != nil {
			t.Fatalf("AddString: %v", err)
		}
	}
	if !h.Equal(h2) {
		t.Fatal("insertion order must not affect the registers")
	}
}

func TestHLLCountError(t *testing.T) {
	const p = 14
	stdErr, err := StandardError(p)
	if err != nil {
		t.Fatalf("StandardError: %v", err)
	}
	// Allow a generous multiple of the expected relative standard error.
	tolerance := 4 * stdErr

	for _, n := range []int{1, 2, 10, 100, 1000, 10000, 50000, 200000} {
		h, err := New(p)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		fill(t, h, fmt.Sprintf("err-%d", n), n)

		got := h.Count()
		relErr := h.RelativeError(uint64(n))
		if math.Abs(relErr) > tolerance {
			t.Fatalf("n=%d: Count = %d, relative error %+.5f exceeds %.5f", n, got, relErr, tolerance)
		}
		if n <= 10 && got != uint64(n) {
			t.Fatalf("n=%d: Count = %d, want exactly %d in the sparse regime", n, got, n)
		}
	}

	low, err := New(4)
	if err != nil {
		t.Fatalf("New(4): %v", err)
	}
	lowStdErr, err := StandardError(4)
	if err != nil {
		t.Fatalf("StandardError(4): %v", err)
	}
	if lowStdErr <= stdErr {
		t.Fatalf("standard error at p=4 (%.5f) must exceed p=14 (%.5f)", lowStdErr, stdErr)
	}
	fill(t, low, "coarse", 10000)
	if relErr := low.RelativeError(10000); math.Abs(relErr) > 4*lowStdErr {
		t.Fatalf("p=4: relative error %+.5f exceeds %.5f", relErr, 4*lowStdErr)
	}
}

func TestHLLMerge(t *testing.T) {
	const p = 14

	left, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	right, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	combined, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	for i := 0; i < 30000; i++ {
		item := fmt.Sprintf("left-%d", i)
		if err := left.AddString(item); err != nil {
			t.Fatalf("AddString: %v", err)
		}
		if err := combined.AddString(item); err != nil {
			t.Fatalf("AddString: %v", err)
		}
	}
	// Overlap the two halves so that the union is smaller than the sum.
	for i := 20000; i < 60000; i++ {
		item := fmt.Sprintf("left-%d", i)
		if err := right.AddString(item); err != nil {
			t.Fatalf("AddString: %v", err)
		}
		if err := combined.AddString(item); err != nil {
			t.Fatalf("AddString: %v", err)
		}
	}

	rightBefore := right.Registers()
	rightCount := right.Count()

	if err := left.Merge(right); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	if !left.Equal(combined) {
		t.Fatal("merged registers must equal those of a sketch fed both streams")
	}
	if got, want := left.Count(), combined.Count(); got != want {
		t.Fatalf("merged Count = %d, single-pass Count = %d", got, want)
	}

	rightAfter := right.Registers()
	for i := range rightBefore {
		if rightBefore[i] != rightAfter[i] {
			t.Fatalf("Merge modified the operand at register %d", i)
		}
	}
	if got := right.Count(); got != rightCount {
		t.Fatalf("Merge moved the operand Count from %d to %d", rightCount, got)
	}

	if err := left.Merge(nil); !errors.Is(err, ErrNilSketch) {
		t.Fatalf("Merge(nil) = %v, want ErrNilSketch", err)
	}
	other, err := New(12)
	if err != nil {
		t.Fatalf("New(12): %v", err)
	}
	if err := left.Merge(other); !errors.Is(err, ErrPrecisionMismatch) {
		t.Fatalf("Merge across precisions = %v, want ErrPrecisionMismatch", err)
	}

	selfCount := left.Count()
	if err := left.Merge(left); err != nil {
		t.Fatalf("Merge(self): %v", err)
	}
	if got := left.Count(); got != selfCount {
		t.Fatalf("Merge(self) moved Count from %d to %d", selfCount, got)
	}

	union, err := Union(right, combined)
	if err != nil {
		t.Fatalf("Union: %v", err)
	}
	if got := right.Count(); got != rightCount {
		t.Fatalf("Union modified its first operand: Count %d, want %d", got, rightCount)
	}
	if union.Count() < combined.Count() {
		t.Fatalf("Union Count %d must be at least %d", union.Count(), combined.Count())
	}
	if _, err := Union(nil, combined); !errors.Is(err, ErrNilSketch) {
		t.Fatalf("Union(nil, x) = %v, want ErrNilSketch", err)
	}
}

func TestHLLMergeSparseStaysSparse(t *testing.T) {
	const p = 14

	a, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	fill(t, a, "small-a", 40)
	fill(t, b, "small-b", 40)

	if !a.IsSparse() || !b.IsSparse() {
		t.Fatal("40 elements must leave a p=14 sketch sparse")
	}
	if err := a.Merge(b); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if !a.IsSparse() {
		t.Fatal("merging two sparse sketches well under the limit must stay sparse")
	}
	if got, want := a.Count(), uint64(80); got < want-4 || got > want+4 {
		t.Fatalf("merged Count = %d, want near %d", got, want)
	}

	dense, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	fill(t, dense, "big", 20000)
	if dense.IsSparse() {
		t.Fatal("20000 elements must promote a p=14 sketch")
	}

	sparseSide, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	fill(t, sparseSide, "small-a", 40)
	if err := sparseSide.Merge(dense); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if sparseSide.IsSparse() {
		t.Fatal("merging a dense sketch in must promote the receiver")
	}
	if sparseSide.SparseSize() != 0 {
		t.Fatalf("SparseSize after promotion = %d, want 0", sparseSide.SparseSize())
	}
}

func TestHLLSparseDense(t *testing.T) {
	const p = 12
	h, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	limit := h.SparseLimit()
	if limit <= 0 || limit >= int(h.RegisterCount()) {
		t.Fatalf("SparseLimit = %d, want within (0,%d)", limit, h.RegisterCount())
	}
	if !h.IsSparse() {
		t.Fatal("a fresh sketch must start sparse")
	}

	fill(t, h, "sd", 200)
	if !h.IsSparse() {
		t.Fatal("200 elements must stay under the sparse limit at p=12")
	}
	if got := h.SparseSize(); got == 0 || got > limit {
		t.Fatalf("SparseSize = %d, want within (0,%d]", got, limit)
	}

	// Promotion must not move the estimate: the estimator reads register
	// values, which the two representations hold identically.
	sparseCount := h.Count()
	promoted := h.Clone()
	promoted.Densify()
	if promoted.IsSparse() {
		t.Fatal("Densify must leave the sketch dense")
	}
	if got := promoted.Count(); got != sparseCount {
		t.Fatalf("Count after Densify = %d, want %d", got, sparseCount)
	}
	if !h.Equal(promoted) {
		t.Fatal("Densify must preserve every register value")
	}
	if !h.IsSparse() {
		t.Fatal("Densify on a clone must not touch the original")
	}

	// Cross the limit and confirm the sketch flips exactly once.
	flips := 0
	wasSparse := h.IsSparse()
	for i := 200; i < 6000; i++ {
		if err := h.AddString(fmt.Sprintf("sd-%d", i)); err != nil {
			t.Fatalf("AddString: %v", err)
		}
		if h.IsSparse() != wasSparse {
			flips++
			wasSparse = h.IsSparse()
		}
		if h.IsSparse() && h.SparseSize() > limit {
			t.Fatalf("at %d elements SparseSize %d exceeds the limit %d without promoting", i+1, h.SparseSize(), limit)
		}
	}
	if flips != 1 {
		t.Fatalf("representation flipped %d times, want exactly 1", flips)
	}
	if h.IsSparse() {
		t.Fatal("6000 elements must promote a p=12 sketch")
	}
	if got := h.SparseSize(); got != 0 {
		t.Fatalf("SparseSize on a dense sketch = %d, want 0", got)
	}
	if got := len(h.Registers()); got != int(h.RegisterCount()) {
		t.Fatalf("Registers length = %d, want %d", got, h.RegisterCount())
	}

	relErr := h.RelativeError(5800)
	stdErr, err := StandardError(p)
	if err != nil {
		t.Fatalf("StandardError: %v", err)
	}
	if math.Abs(relErr) > 4*stdErr {
		t.Fatalf("after promotion relative error %+.5f exceeds %.5f", relErr, 4*stdErr)
	}

	// A sketch fed the same stream but densified up front must agree exactly.
	eager, err := New(p)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	eager.Densify()
	fill(t, eager, "sd", 6000)
	if !eager.Equal(h) {
		t.Fatal("promoting lazily and eagerly must give the same registers")
	}
	if got, want := eager.Count(), h.Count(); got != want {
		t.Fatalf("eager Count = %d, lazy Count = %d", got, want)
	}
}

func TestHLLPrecisionValidation(t *testing.T) {
	for _, p := range []uint{0, 1, 3, 19, 32, 100} {
		if _, err := New(p); !errors.Is(err, ErrPrecisionRange) {
			t.Fatalf("New(%d) = %v, want ErrPrecisionRange", p, err)
		}
		if _, err := StandardError(p); !errors.Is(err, ErrPrecisionRange) {
			t.Fatalf("StandardError(%d) = %v, want ErrPrecisionRange", p, err)
		}
	}

	for p := MinPrecision; p <= MaxPrecision; p++ {
		h, err := New(p)
		if err != nil {
			t.Fatalf("New(%d): %v", p, err)
		}
		if h.Precision() != p {
			t.Fatalf("Precision = %d, want %d", h.Precision(), p)
		}
		if got, want := h.RegisterCount(), uint32(1)<<p; got != want {
			t.Fatalf("p=%d: RegisterCount = %d, want %d", p, got, want)
		}
		if got := h.SparseLimit(); got >= int(h.RegisterCount()) {
			t.Fatalf("p=%d: SparseLimit %d must stay below m=%d", p, got, h.RegisterCount())
		}
	}

	if got := NewDefault().Precision(); got != DefaultPrecision {
		t.Fatalf("NewDefault precision = %d, want %d", got, DefaultPrecision)
	}
}

func TestHLLRejectsEmptyItem(t *testing.T) {
	h := NewDefault()

	if err := h.AddString(""); !errors.Is(err, ErrEmptyItem) {
		t.Fatalf("AddString(\"\") = %v, want ErrEmptyItem", err)
	}
	if err := h.AddBytes(nil); !errors.Is(err, ErrEmptyItem) {
		t.Fatalf("AddBytes(nil) = %v, want ErrEmptyItem", err)
	}
	if err := h.AddBytes([]byte{}); !errors.Is(err, ErrEmptyItem) {
		t.Fatalf("AddBytes(empty) = %v, want ErrEmptyItem", err)
	}
	if got := h.Count(); got != 0 {
		t.Fatalf("rejected items must not be counted, Count = %d", got)
	}
	if got := h.SparseSize(); got != 0 {
		t.Fatalf("rejected items must not occupy a register, SparseSize = %d", got)
	}

	if err := h.AddBytes([]byte{0}); err != nil {
		t.Fatalf("AddBytes([]byte{0}): %v", err)
	}
	if got := h.Count(); got != 1 {
		t.Fatalf("a single NUL byte is a real item, Count = %d, want 1", got)
	}

	if err := h.AddString("x"); err != nil {
		t.Fatalf("AddString: %v", err)
	}
	if got, want := h.Count(), uint64(2); got != want {
		t.Fatalf("Count = %d, want %d", got, want)
	}
	if got := h.AddBytes([]byte("x")); got != nil {
		t.Fatalf("AddBytes: %v", got)
	}
	if got, want := h.Count(), uint64(2); got != want {
		t.Fatalf("AddBytes must agree with AddString: Count = %d, want %d", got, want)
	}
}

func TestHLLAddLines(t *testing.T) {
	h := NewDefault()

	input := "alpha\nbeta\n\n  gamma  \nalpha\n\t\ndelta\n"
	added, err := h.AddLines(strings.NewReader(input))
	if err != nil {
		t.Fatalf("AddLines: %v", err)
	}
	if want := 5; added != want {
		t.Fatalf("AddLines added %d, want %d", added, want)
	}
	if got, want := h.Count(), uint64(4); got != want {
		t.Fatalf("Count = %d, want %d distinct", got, want)
	}

	trimmed := NewDefault()
	if _, err := trimmed.AddLines(strings.NewReader("gamma\n")); err != nil {
		t.Fatalf("AddLines: %v", err)
	}
	if got := trimmed.Count(); got != 1 {
		t.Fatalf("Count = %d, want 1", got)
	}

	empty := NewDefault()
	added, err = empty.AddLines(strings.NewReader(""))
	if err != nil {
		t.Fatalf("AddLines on empty input: %v", err)
	}
	if added != 0 {
		t.Fatalf("AddLines added %d on empty input, want 0", added)
	}
	if got := empty.Count(); got != 0 {
		t.Fatalf("Count = %d on empty input, want 0", got)
	}

	blanks := NewDefault()
	added, err = blanks.AddLines(strings.NewReader("\n\n   \n\t\n"))
	if err != nil {
		t.Fatalf("AddLines on blank input: %v", err)
	}
	if added != 0 {
		t.Fatalf("blank lines must be skipped, added %d", added)
	}

	noTrailing := NewDefault()
	added, err = noTrailing.AddLines(strings.NewReader("one\ntwo"))
	if err != nil {
		t.Fatalf("AddLines: %v", err)
	}
	if added != 2 {
		t.Fatalf("a missing final newline must not drop the last item, added %d", added)
	}

	if _, err := h.AddLines(nil); err == nil {
		t.Fatal("AddLines(nil) must fail")
	}
}

func TestHLLCodecRoundTrip(t *testing.T) {
	for _, n := range []int{0, 1, 50, 900, 20000} {
		for _, p := range []uint{8, 12, 14} {
			h, err := New(p)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			fill(t, h, fmt.Sprintf("codec-%d", n), n)

			blob, err := h.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary: %v", err)
			}
			if len(blob) == 0 {
				t.Fatal("MarshalBinary produced no bytes")
			}

			back, err := Unmarshal(blob)
			if err != nil {
				t.Fatalf("n=%d p=%d: Unmarshal: %v", n, p, err)
			}
			if back.Precision() != p {
				t.Fatalf("decoded precision = %d, want %d", back.Precision(), p)
			}
			if back.IsSparse() != h.IsSparse() {
				t.Fatalf("n=%d p=%d: decoded sparse = %v, want %v", n, p, back.IsSparse(), h.IsSparse())
			}
			if !h.Equal(back) {
				t.Fatalf("n=%d p=%d: registers changed across the round trip", n, p)
			}
			if got, want := back.Count(), h.Count(); got != want {
				t.Fatalf("n=%d p=%d: decoded Count = %d, want %d", n, p, got, want)
			}

			again, err := back.MarshalBinary()
			if err != nil {
				t.Fatalf("MarshalBinary: %v", err)
			}
			if string(again) != string(blob) {
				t.Fatalf("n=%d p=%d: encoding is not canonical", n, p)
			}

			// A decode into a live sketch must replace its contents.
			target := NewDefault()
			fill(t, target, "victim", 10)
			if err := target.UnmarshalBinary(blob); err != nil {
				t.Fatalf("UnmarshalBinary: %v", err)
			}
			if !target.Equal(h) {
				t.Fatal("UnmarshalBinary must replace the receiver's registers")
			}
		}
	}
}

func TestHLLCodecRejectsCorrupt(t *testing.T) {
	h := NewDefault()
	fill(t, h, "corrupt", 100)
	blob, err := h.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	if _, err := Unmarshal(nil); !errors.Is(err, ErrTruncated) {
		t.Fatalf("Unmarshal(nil) = %v, want ErrTruncated", err)
	}
	if _, err := Unmarshal(blob[:4]); !errors.Is(err, ErrTruncated) {
		t.Fatalf("Unmarshal(header only) = %v, want ErrTruncated", err)
	}
	if _, err := Unmarshal(blob[:len(blob)-1]); !errors.Is(err, ErrTruncated) {
		t.Fatalf("Unmarshal(short body) = %v, want ErrTruncated", err)
	}

	extra := append(append([]byte{}, blob...), 0)
	if _, err := Unmarshal(extra); !errors.Is(err, ErrTrailingBytes) {
		t.Fatalf("Unmarshal(long body) = %v, want ErrTrailingBytes", err)
	}

	badMagic := append([]byte{}, blob...)
	badMagic[0] = 'X'
	if _, err := Unmarshal(badMagic); !errors.Is(err, ErrBadMagic) {
		t.Fatalf("Unmarshal(bad magic) = %v, want ErrBadMagic", err)
	}

	badVersion := append([]byte{}, blob...)
	badVersion[magicLen] = wireVersion + 7
	if _, err := Unmarshal(badVersion); !errors.Is(err, ErrBadVersion) {
		t.Fatalf("Unmarshal(bad version) = %v, want ErrBadVersion", err)
	}

	badPrecision := append([]byte{}, blob...)
	badPrecision[magicLen+1] = 63
	if _, err := Unmarshal(badPrecision); !errors.Is(err, ErrPrecisionRange) {
		t.Fatalf("Unmarshal(bad precision) = %v, want ErrPrecisionRange", err)
	}

	badMode := append([]byte{}, blob...)
	badMode[magicLen+2] = 9
	if _, err := Unmarshal(badMode); !errors.Is(err, ErrBadMode) {
		t.Fatalf("Unmarshal(bad mode) = %v, want ErrBadMode", err)
	}

	badIndex := append([]byte{}, blob...)
	badIndex[headerLen+4] = 0xff
	if _, err := Unmarshal(badIndex); !errors.Is(err, ErrIndexOutOfRange) {
		t.Fatalf("Unmarshal(index past m) = %v, want ErrIndexOutOfRange", err)
	}

	badRho := append([]byte{}, blob...)
	badRho[headerLen+4+sparseEntryLen-1] = 200
	if _, err := Unmarshal(badRho); !errors.Is(err, ErrRhoOutOfRange) {
		t.Fatalf("Unmarshal(oversized register) = %v, want ErrRhoOutOfRange", err)
	}

	dense := h.Clone()
	dense.Densify()
	denseBlob, err := dense.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	badDense := append([]byte{}, denseBlob...)
	badDense[headerLen] = 200
	if _, err := Unmarshal(badDense); !errors.Is(err, ErrRhoOutOfRange) {
		t.Fatalf("Unmarshal(oversized dense register) = %v, want ErrRhoOutOfRange", err)
	}

	// A rejected decode must leave the receiver untouched.
	survivor := NewDefault()
	fill(t, survivor, "survivor", 30)
	before := survivor.Count()
	if err := survivor.UnmarshalBinary(badMagic); err == nil {
		t.Fatal("UnmarshalBinary must reject a bad blob")
	}
	if got := survivor.Count(); got != before {
		t.Fatalf("a failed decode moved Count from %d to %d", before, got)
	}
}

func TestHLLCloneResetStats(t *testing.T) {
	h := NewDefault()
	fill(t, h, "clone", 3000)
	baseline := h.Count()

	clone := h.Clone()
	if !clone.Equal(h) {
		t.Fatal("Clone must copy every register")
	}
	fill(t, clone, "extra", 3000)
	if got := h.Count(); got != baseline {
		t.Fatalf("writing to the clone moved the original Count from %d to %d", baseline, got)
	}
	if clone.Count() <= baseline {
		t.Fatalf("clone Count %d must exceed %d after new elements", clone.Count(), baseline)
	}

	stats := h.Stats()
	if stats.Precision != DefaultPrecision {
		t.Fatalf("Stats.Precision = %d, want %d", stats.Precision, DefaultPrecision)
	}
	if stats.Registers != h.RegisterCount() {
		t.Fatalf("Stats.Registers = %d, want %d", stats.Registers, h.RegisterCount())
	}
	if stats.Occupied+stats.Zeros != stats.Registers {
		t.Fatalf("Occupied %d + Zeros %d != Registers %d", stats.Occupied, stats.Zeros, stats.Registers)
	}
	if stats.Estimate != baseline {
		t.Fatalf("Stats.Estimate = %d, want %d", stats.Estimate, baseline)
	}
	if stats.MaxRegister == 0 {
		t.Fatal("Stats.MaxRegister must be positive for a populated sketch")
	}
	if stats.StandardError <= 0 {
		t.Fatalf("Stats.StandardError = %v, want positive", stats.StandardError)
	}
	if stats.Sparse != h.IsSparse() {
		t.Fatalf("Stats.Sparse = %v, want %v", stats.Sparse, h.IsSparse())
	}

	registers := h.Registers()
	for i := range registers {
		registers[i] = 99
	}
	if h.Count() != baseline {
		t.Fatal("Registers must return a copy the caller can scribble on")
	}

	h.Reset()
	if got := h.Count(); got != 0 {
		t.Fatalf("Count after Reset = %d, want 0", got)
	}
	if !h.IsSparse() {
		t.Fatal("Reset must return the sketch to the sparse representation")
	}
	if got := h.SparseSize(); got != 0 {
		t.Fatalf("SparseSize after Reset = %d, want 0", got)
	}
	if h.Precision() != DefaultPrecision {
		t.Fatal("Reset must keep the precision")
	}
	if h.Equal(nil) {
		t.Fatal("Equal(nil) must be false")
	}
	if got := h.RelativeError(0); got != 0 {
		t.Fatalf("RelativeError(0) on an empty sketch = %v, want 0", got)
	}
}

func TestHLLEstimatorHelpers(t *testing.T) {
	if got, want := Alpha(16), 0.673; got != want {
		t.Fatalf("Alpha(16) = %v, want %v", got, want)
	}
	if got, want := Alpha(32), 0.697; got != want {
		t.Fatalf("Alpha(32) = %v, want %v", got, want)
	}
	if got, want := Alpha(64), 0.709; got != want {
		t.Fatalf("Alpha(64) = %v, want %v", got, want)
	}
	large := Alpha(16384)
	if large <= 0.70 || large >= 0.7213 {
		t.Fatalf("Alpha(16384) = %v, want within (0.70,0.7213)", large)
	}

	if got := LinearCounting(0, 0); got != 0 {
		t.Fatalf("LinearCounting(0,0) = %v, want 0", got)
	}
	if got := LinearCounting(16, 16); got != 0 {
		t.Fatalf("LinearCounting(m,m) = %v, want 0", got)
	}
	saturated := LinearCounting(16, 0)
	if saturated <= 0 || math.IsInf(saturated, 0) {
		t.Fatalf("LinearCounting(16,0) = %v, want a finite positive value", saturated)
	}
	if got := LinearCounting(16, 20); got != 0 {
		t.Fatalf("LinearCounting must clamp zeros above m, got %v", got)
	}
	if LinearCounting(1024, 512) >= LinearCounting(1024, 256) {
		t.Fatal("LinearCounting must fall as more registers stay empty")
	}

	if got := RawEstimate(nil); got != 0 {
		t.Fatalf("RawEstimate(nil) = %v, want 0", got)
	}
	if got := CountZeros(nil); got != 0 {
		t.Fatalf("CountZeros(nil) = %v, want 0", got)
	}
	registers := []uint8{0, 1, 0, 3}
	if got, want := CountZeros(registers), uint32(2); got != want {
		t.Fatalf("CountZeros = %d, want %d", got, want)
	}
	if RawEstimate(registers) <= 0 {
		t.Fatal("RawEstimate must be positive when a register is set")
	}

	coarse, err := StandardError(10)
	if err != nil {
		t.Fatalf("StandardError(10): %v", err)
	}
	fine, err := StandardError(16)
	if err != nil {
		t.Fatalf("StandardError(16): %v", err)
	}
	if coarse <= fine {
		t.Fatalf("standard error must fall with precision: p=10 %v, p=16 %v", coarse, fine)
	}
}

func TestHLLAddHashedMatchesAddString(t *testing.T) {
	direct := NewDefault()
	viaHash := NewDefault()

	for i := 0; i < 2000; i++ {
		item := fmt.Sprintf("hashed-%d", i)
		if err := direct.AddString(item); err != nil {
			t.Fatalf("AddString: %v", err)
		}
		if err := viaHash.Add(hash.Hash64String(item)); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	if !direct.Equal(viaHash) {
		t.Fatal("Add on a precomputed digest must match AddString")
	}
	if got, want := viaHash.Count(), direct.Count(); got != want {
		t.Fatalf("Count = %d, want %d", got, want)
	}
}
