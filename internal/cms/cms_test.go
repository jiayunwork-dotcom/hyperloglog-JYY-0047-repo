package cms

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
)

// addN records n occurrences of item one at a time.
func addN(t *testing.T, s *Sketch, item string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := s.AddString(item); err != nil {
			t.Fatalf("AddString(%q): %v", item, err)
		}
	}
}

func TestCMSAddEstimate(t *testing.T) {
	s, err := New(5, 4096)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if got := s.Total(); got != 0 {
		t.Fatalf("Total on a fresh sketch = %d, want 0", got)
	}
	if got := s.Distinct(); got != 0 {
		t.Fatalf("Distinct on a fresh sketch = %d, want 0", got)
	}
	if got := s.EstimateString("absent"); got != 0 {
		t.Fatalf("Estimate of an unseen item = %d, want 0", got)
	}

	counts := map[string]int{
		"alpha":   17,
		"beta":    5,
		"gamma":   1,
		"delta":   42,
		"epsilon": 3,
	}
	total := 0
	for item, n := range counts {
		addN(t, s, item, n)
		total += n
	}

	if got := s.Total(); got != uint64(total) {
		t.Fatalf("Total = %d, want %d", got, total)
	}
	if got, want := s.Distinct(), len(counts); got != want {
		t.Fatalf("Distinct = %d, want %d", got, want)
	}

	// A sketch this wide holds five elements without collisions, so the
	// estimates are exact. In general they may only overshoot.
	for item, want := range counts {
		got := s.EstimateString(item)
		if got != uint64(want) {
			t.Fatalf("Estimate(%q) = %d, want %d", item, got, want)
		}
		if !s.Seen(item) {
			t.Fatalf("Seen(%q) = false, want true", item)
		}
	}
	if s.Seen("absent") {
		t.Fatal("Seen on an unrecorded item must be false")
	}

	if err := s.AddCount([]byte("alpha"), 100); err != nil {
		t.Fatalf("AddCount: %v", err)
	}
	if got, want := s.EstimateString("alpha"), uint64(117); got != want {
		t.Fatalf("Estimate after AddCount = %d, want %d", got, want)
	}
	if got, want := s.Total(), uint64(total+100); got != want {
		t.Fatalf("Total after AddCount = %d, want %d", got, want)
	}

	if err := s.AddCount([]byte("zeta"), 0); err != nil {
		t.Fatalf("AddCount with zero: %v", err)
	}
	if got := s.EstimateString("zeta"); got != 0 {
		t.Fatalf("Estimate of a zero-count item = %d, want 0", got)
	}
	if !s.Seen("zeta") {
		t.Fatal("a zero-count item must still be recorded as seen")
	}
	if got, want := s.Total(), uint64(total+100); got != want {
		t.Fatalf("a zero count must not move Total: %d, want %d", got, want)
	}

	if got := s.Estimate(nil); got != 0 {
		t.Fatalf("Estimate(nil) = %d, want 0", got)
	}
	if got, want := s.EstimateString("alpha"), s.Estimate([]byte("alpha")); got != want {
		t.Fatalf("EstimateString = %d, Estimate = %d", got, want)
	}
}

func TestCMSNeverUndercounts(t *testing.T) {
	// A narrow matrix forces collisions, which is exactly where the
	// one-sided guarantee matters.
	s, err := New(4, 64)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	truth := make(map[string]uint64, 500)
	for i := 0; i < 500; i++ {
		item := fmt.Sprintf("item-%d", i)
		n := uint64(i%7 + 1)
		if err := s.AddCount([]byte(item), n); err != nil {
			t.Fatalf("AddCount: %v", err)
		}
		truth[item] = n
	}

	overshoots := 0
	for item, want := range truth {
		got := s.EstimateString(item)
		if got < want {
			t.Fatalf("Estimate(%q) = %d, below the true count %d", item, got, want)
		}
		if got > want {
			overshoots++
		}
	}
	if overshoots == 0 {
		t.Fatal("a 4x64 matrix over 500 items must collide somewhere")
	}

	if load := s.Load(); load <= 0 || load > 1 {
		t.Fatalf("Load = %v, want within (0,1]", load)
	}

	wide, err := New(4, 8192)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for item, n := range truth {
		if err := wide.AddCount([]byte(item), n); err != nil {
			t.Fatalf("AddCount: %v", err)
		}
	}
	if wide.Load() >= s.Load() {
		t.Fatalf("a wider matrix must run at a lower load: wide %v, narrow %v", wide.Load(), s.Load())
	}

	wideError := 0
	for item, want := range truth {
		if wide.EstimateString(item) != want {
			wideError++
		}
	}
	if wideError >= overshoots {
		t.Fatalf("widening the matrix must reduce overshoot: wide %d, narrow %d", wideError, overshoots)
	}
}

func TestCMSHeavyHitters(t *testing.T) {
	s, err := New(5, 4096)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	empty, err := s.HeavyHitters(0.1)
	if err != nil {
		t.Fatalf("HeavyHitters on an empty sketch: %v", err)
	}
	if empty == nil {
		t.Fatal("HeavyHitters must return a non-nil slice")
	}
	if len(empty) != 0 {
		t.Fatalf("HeavyHitters on an empty sketch returned %d entries", len(empty))
	}

	addN(t, s, "hot", 700)
	addN(t, s, "warm", 200)
	addN(t, s, "mild", 60)
	addN(t, s, "cold", 30)
	addN(t, s, "rare", 10)
	const total = 1000

	if got := s.Total(); got != total {
		t.Fatalf("Total = %d, want %d", got, total)
	}

	hitters, err := s.HeavyHitters(0.15)
	if err != nil {
		t.Fatalf("HeavyHitters: %v", err)
	}
	if len(hitters) != 2 {
		t.Fatalf("HeavyHitters(0.15) returned %d entries, want 2: %+v", len(hitters), hitters)
	}
	if hitters[0].Item != "hot" || hitters[1].Item != "warm" {
		t.Fatalf("HeavyHitters(0.15) = %+v, want hot then warm", hitters)
	}
	if hitters[0].Estimate != 700 || hitters[1].Estimate != 200 {
		t.Fatalf("estimates = %d,%d, want 700,200", hitters[0].Estimate, hitters[1].Estimate)
	}
	if math.Abs(hitters[0].Share-0.7) > 1e-9 {
		t.Fatalf("Share = %v, want 0.7", hitters[0].Share)
	}
	if math.Abs(hitters[1].Share-0.2) > 1e-9 {
		t.Fatalf("Share = %v, want 0.2", hitters[1].Share)
	}

	// Descending by estimate.
	for i := 1; i < len(hitters); i++ {
		if hitters[i-1].Estimate < hitters[i].Estimate {
			t.Fatalf("results not ordered by descending estimate: %+v", hitters)
		}
	}

	// The threshold is strict: "warm" sits exactly on 0.2 and drops out.
	exact, err := s.HeavyHitters(0.2)
	if err != nil {
		t.Fatalf("HeavyHitters: %v", err)
	}
	if len(exact) != 1 || exact[0].Item != "hot" {
		t.Fatalf("HeavyHitters(0.2) = %+v, want only hot", exact)
	}

	all, err := s.HeavyHitters(0)
	if err != nil {
		t.Fatalf("HeavyHitters(0): %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("HeavyHitters(0) returned %d entries, want 5", len(all))
	}

	none, err := s.HeavyHitters(1)
	if err != nil {
		t.Fatalf("HeavyHitters(1): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("HeavyHitters(1) returned %d entries, want 0", len(none))
	}

	for _, phi := range []float64{-0.1, 1.5, math.NaN()} {
		if _, err := s.HeavyHitters(phi); !errors.Is(err, ErrPhiRange) {
			t.Fatalf("HeavyHitters(%v) = %v, want ErrPhiRange", phi, err)
		}
	}

	// Repeated calls must give byte-identical ordering.
	first, err := s.HeavyHitters(0)
	if err != nil {
		t.Fatalf("HeavyHitters: %v", err)
	}
	for round := 0; round < 5; round++ {
		again, err := s.HeavyHitters(0)
		if err != nil {
			t.Fatalf("HeavyHitters: %v", err)
		}
		if len(again) != len(first) {
			t.Fatalf("round %d: length changed", round)
		}
		for i := range first {
			if again[i] != first[i] {
				t.Fatalf("round %d: entry %d changed from %+v to %+v", round, i, first[i], again[i])
			}
		}
	}

	tied, err := New(5, 4096)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, item := range []string{"delta", "alpha", "charlie", "bravo"} {
		addN(t, tied, item, 25)
	}
	ordered, err := tied.HeavyHitters(0)
	if err != nil {
		t.Fatalf("HeavyHitters: %v", err)
	}
	want := []string{"alpha", "bravo", "charlie", "delta"}
	if len(ordered) != len(want) {
		t.Fatalf("got %d entries, want %d", len(ordered), len(want))
	}
	for i := range want {
		if ordered[i].Item != want[i] {
			t.Fatalf("tie order = %+v, want %v", ordered, want)
		}
	}
}

func TestCMSTopK(t *testing.T) {
	s := NewDefault()
	addN(t, s, "a", 50)
	addN(t, s, "b", 40)
	addN(t, s, "c", 30)
	addN(t, s, "d", 20)

	top, err := s.TopK(2)
	if err != nil {
		t.Fatalf("TopK: %v", err)
	}
	if len(top) != 2 {
		t.Fatalf("TopK(2) returned %d entries, want 2", len(top))
	}
	if top[0].Item != "a" || top[1].Item != "b" {
		t.Fatalf("TopK(2) = %+v, want a then b", top)
	}

	zero, err := s.TopK(0)
	if err != nil {
		t.Fatalf("TopK(0): %v", err)
	}
	if zero == nil || len(zero) != 0 {
		t.Fatalf("TopK(0) = %+v, want an empty non-nil slice", zero)
	}

	over, err := s.TopK(100)
	if err != nil {
		t.Fatalf("TopK(100): %v", err)
	}
	if len(over) != 4 {
		t.Fatalf("TopK(100) returned %d entries, want 4", len(over))
	}

	if _, err := s.TopK(-1); !errors.Is(err, ErrPhiRange) {
		t.Fatalf("TopK(-1) = %v, want ErrPhiRange", err)
	}

	fresh := NewDefault()
	none, err := fresh.TopK(3)
	if err != nil {
		t.Fatalf("TopK on an empty sketch: %v", err)
	}
	if none == nil || len(none) != 0 {
		t.Fatalf("TopK on an empty sketch = %+v, want an empty non-nil slice", none)
	}

	if err := fresh.AddCount([]byte("weightless"), 0); err != nil {
		t.Fatalf("AddCount: %v", err)
	}
	skipped, err := fresh.TopK(3)
	if err != nil {
		t.Fatalf("TopK: %v", err)
	}
	if len(skipped) != 0 {
		t.Fatalf("a zero-estimate item must not appear in TopK: %+v", skipped)
	}
}

func TestCMSMerge(t *testing.T) {
	left, err := New(5, 2048)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	right, err := New(5, 2048)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	combined, err := New(5, 2048)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	leftCounts := map[string]int{"shared": 10, "left-only": 25}
	rightCounts := map[string]int{"shared": 7, "right-only": 13}
	for item, n := range leftCounts {
		addN(t, left, item, n)
		addN(t, combined, item, n)
	}
	for item, n := range rightCounts {
		addN(t, right, item, n)
		addN(t, combined, item, n)
	}

	rightRow, err := right.Row(0)
	if err != nil {
		t.Fatalf("Row: %v", err)
	}
	rightTotal := right.Total()
	rightDistinct := right.Distinct()

	if err := left.Merge(right); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	if got, want := left.Total(), combined.Total(); got != want {
		t.Fatalf("merged Total = %d, want %d", got, want)
	}
	if got, want := left.Distinct(), combined.Distinct(); got != want {
		t.Fatalf("merged Distinct = %d, want %d", got, want)
	}
	for _, item := range []string{"shared", "left-only", "right-only"} {
		got := left.EstimateString(item)
		want := combined.EstimateString(item)
		if got != want {
			t.Fatalf("merged Estimate(%q) = %d, single-pass = %d", item, got, want)
		}
		if !left.Seen(item) {
			t.Fatalf("merged sketch must have seen %q", item)
		}
	}
	if got, want := left.EstimateString("shared"), uint64(17); got != want {
		t.Fatalf("Estimate(shared) = %d, want %d", got, want)
	}

	for i := uint32(0); i < left.Depth(); i++ {
		l, err := left.Row(i)
		if err != nil {
			t.Fatalf("Row: %v", err)
		}
		c, err := combined.Row(i)
		if err != nil {
			t.Fatalf("Row: %v", err)
		}
		for j := range l {
			if l[j] != c[j] {
				t.Fatalf("row %d col %d = %d, single-pass = %d", i, j, l[j], c[j])
			}
		}
	}

	afterRow, err := right.Row(0)
	if err != nil {
		t.Fatalf("Row: %v", err)
	}
	for j := range rightRow {
		if rightRow[j] != afterRow[j] {
			t.Fatalf("Merge modified the operand at column %d", j)
		}
	}
	if got := right.Total(); got != rightTotal {
		t.Fatalf("Merge moved the operand Total from %d to %d", rightTotal, got)
	}
	if got := right.Distinct(); got != rightDistinct {
		t.Fatalf("Merge moved the operand Distinct from %d to %d", rightDistinct, got)
	}

	if err := left.Merge(nil); !errors.Is(err, ErrNilSketch) {
		t.Fatalf("Merge(nil) = %v, want ErrNilSketch", err)
	}
	narrow, err := New(5, 1024)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := left.Merge(narrow); !errors.Is(err, ErrDimensionMismatch) {
		t.Fatalf("Merge across widths = %v, want ErrDimensionMismatch", err)
	}
	shallow, err := New(3, 2048)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := left.Merge(shallow); !errors.Is(err, ErrDimensionMismatch) {
		t.Fatalf("Merge across depths = %v, want ErrDimensionMismatch", err)
	}

	selfTotal := left.Total()
	if err := left.Merge(left); err != nil {
		t.Fatalf("Merge(self): %v", err)
	}
	if got := left.Total(); got != selfTotal {
		t.Fatalf("Merge(self) moved Total from %d to %d", selfTotal, got)
	}

	union, err := Union(right, combined)
	if err != nil {
		t.Fatalf("Union: %v", err)
	}
	if got := right.Total(); got != rightTotal {
		t.Fatalf("Union modified its first operand: Total %d, want %d", got, rightTotal)
	}
	if got, want := union.Total(), rightTotal+combined.Total(); got != want {
		t.Fatalf("Union Total = %d, want %d", got, want)
	}
	if _, err := Union(nil, combined); !errors.Is(err, ErrNilSketch) {
		t.Fatalf("Union(nil, x) = %v, want ErrNilSketch", err)
	}
}

func TestCMSMergeOverflowLeavesSketchIntact(t *testing.T) {
	left, err := New(2, 8)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	right, err := New(2, 8)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	huge := uint64(math.MaxUint64) - 4
	if err := left.AddCount([]byte("heavy"), huge); err != nil {
		t.Fatalf("AddCount: %v", err)
	}
	if err := right.AddCount([]byte("heavy"), huge); err != nil {
		t.Fatalf("AddCount: %v", err)
	}

	before := left.EstimateString("heavy")
	beforeTotal := left.Total()

	if err := left.Merge(right); !errors.Is(err, ErrCounterOverflow) {
		t.Fatalf("Merge past the counter ceiling = %v, want ErrCounterOverflow", err)
	}
	if got := left.EstimateString("heavy"); got != before {
		t.Fatalf("a rejected Merge changed the estimate from %d to %d", before, got)
	}
	if got := left.Total(); got != beforeTotal {
		t.Fatalf("a rejected Merge changed Total from %d to %d", beforeTotal, got)
	}

	if err := left.AddCount([]byte("heavy"), 10); !errors.Is(err, ErrCounterOverflow) {
		t.Fatalf("AddCount past the counter ceiling = %v, want ErrCounterOverflow", err)
	}
	if got := left.EstimateString("heavy"); got != before {
		t.Fatalf("a rejected AddCount changed the estimate from %d to %d", before, got)
	}
	if got := left.Total(); got != beforeTotal {
		t.Fatalf("a rejected AddCount changed Total from %d to %d", beforeTotal, got)
	}
}

func TestCMSDimensionValidation(t *testing.T) {
	if _, err := New(0, 128); !errors.Is(err, ErrDepthRange) {
		t.Fatalf("New(0,128) = %v, want ErrDepthRange", err)
	}
	if _, err := New(MaxDepth+1, 128); !errors.Is(err, ErrDepthRange) {
		t.Fatalf("New(MaxDepth+1,128) = %v, want ErrDepthRange", err)
	}
	if _, err := New(4, 0); !errors.Is(err, ErrWidthRange) {
		t.Fatalf("New(4,0) = %v, want ErrWidthRange", err)
	}
	if _, err := New(4, MaxWidth+1); !errors.Is(err, ErrWidthRange) {
		t.Fatalf("New(4,MaxWidth+1) = %v, want ErrWidthRange", err)
	}

	s, err := New(MinDepth, MinWidth)
	if err != nil {
		t.Fatalf("New at the lower bounds: %v", err)
	}
	if s.Depth() != MinDepth || s.Width() != MinWidth {
		t.Fatalf("shape = %dx%d, want %dx%d", s.Depth(), s.Width(), MinDepth, MinWidth)
	}
	if err := s.AddString("only"); err != nil {
		t.Fatalf("AddString: %v", err)
	}
	if got := s.EstimateString("only"); got != 1 {
		t.Fatalf("Estimate = %d, want 1", got)
	}

	if _, err := s.Row(s.Depth()); !errors.Is(err, ErrDimensionMismatch) {
		t.Fatalf("Row past the last row = %v, want ErrDimensionMismatch", err)
	}

	def := NewDefault()
	if def.Depth() != DefaultDepth || def.Width() != DefaultWidth {
		t.Fatalf("NewDefault shape = %dx%d, want %dx%d", def.Depth(), def.Width(), DefaultDepth, DefaultWidth)
	}

	if err := def.AddString(""); !errors.Is(err, ErrEmptyItem) {
		t.Fatalf("AddString(\"\") = %v, want ErrEmptyItem", err)
	}
	if err := def.Add(nil); !errors.Is(err, ErrEmptyItem) {
		t.Fatalf("Add(nil) = %v, want ErrEmptyItem", err)
	}
	if err := def.AddCount([]byte{}, 5); !errors.Is(err, ErrEmptyItem) {
		t.Fatalf("AddCount(empty) = %v, want ErrEmptyItem", err)
	}
	if got := def.Total(); got != 0 {
		t.Fatalf("rejected items must not move Total: %d", got)
	}
	if got := def.Distinct(); got != 0 {
		t.Fatalf("rejected items must not be recorded: Distinct = %d", got)
	}
}

func TestCMSAccuracySizing(t *testing.T) {
	s, err := NewWithAccuracy(0.001, 0.01)
	if err != nil {
		t.Fatalf("NewWithAccuracy: %v", err)
	}
	if s.Width() < 2718 {
		t.Fatalf("Width = %d, want at least ceil(e/0.001)", s.Width())
	}
	if s.Depth() < 4 {
		t.Fatalf("Depth = %d, want at least ceil(ln(100))", s.Depth())
	}
	if conf := s.Confidence(); conf <= 0.9 || conf >= 1 {
		t.Fatalf("Confidence = %v, want within (0.9,1)", conf)
	}
	if got := s.ErrorBound(); got != 0 {
		t.Fatalf("ErrorBound on an empty sketch = %v, want 0", got)
	}

	addN(t, s, "x", 1000)
	bound := s.ErrorBound()
	if bound <= 0 {
		t.Fatalf("ErrorBound = %v, want positive", bound)
	}
	if bound >= float64(s.Total()) {
		t.Fatalf("ErrorBound %v must stay below the total %d", bound, s.Total())
	}

	for _, eps := range []float64{0, -0.1, 1, 1.5, math.NaN()} {
		if _, err := NewWithAccuracy(eps, 0.01); !errors.Is(err, ErrEpsilonRange) {
			t.Fatalf("NewWithAccuracy(%v,0.01) = %v, want ErrEpsilonRange", eps, err)
		}
	}
	for _, delta := range []float64{0, -0.1, 1, 2, math.NaN()} {
		if _, err := NewWithAccuracy(0.01, delta); !errors.Is(err, ErrDeltaRange) {
			t.Fatalf("NewWithAccuracy(0.01,%v) = %v, want ErrDeltaRange", delta, err)
		}
	}
	if _, err := NewWithAccuracy(1e-9, 0.01); !errors.Is(err, ErrWidthRange) {
		t.Fatalf("NewWithAccuracy with an unattainable epsilon = %v, want ErrWidthRange", err)
	}

	tight, err := NewWithAccuracy(0.01, 0.5)
	if err != nil {
		t.Fatalf("NewWithAccuracy: %v", err)
	}
	if tight.Depth() < MinDepth {
		t.Fatalf("Depth = %d, want at least %d", tight.Depth(), MinDepth)
	}
	loose, err := NewWithAccuracy(0.1, 0.01)
	if err != nil {
		t.Fatalf("NewWithAccuracy: %v", err)
	}
	if loose.Width() >= tight.Width() {
		t.Fatalf("a looser epsilon must give a narrower matrix: loose %d, tight %d", loose.Width(), tight.Width())
	}
}

func TestCMSAddLines(t *testing.T) {
	s := NewDefault()

	input := "apple\nbanana\napple\n\n  cherry  \napple\n\t\n"
	added, err := s.AddLines(strings.NewReader(input))
	if err != nil {
		t.Fatalf("AddLines: %v", err)
	}
	if want := 5; added != want {
		t.Fatalf("AddLines recorded %d occurrences, want %d", added, want)
	}
	if got, want := s.Total(), uint64(5); got != want {
		t.Fatalf("Total = %d, want %d", got, want)
	}
	if got, want := s.Distinct(), 3; got != want {
		t.Fatalf("Distinct = %d, want %d", got, want)
	}
	if got, want := s.EstimateString("apple"), uint64(3); got != want {
		t.Fatalf("Estimate(apple) = %d, want %d", got, want)
	}
	if got, want := s.EstimateString("cherry"), uint64(1); got != want {
		t.Fatalf("Estimate(cherry) = %d, want %d", got, want)
	}

	items := s.Items()
	want := []string{"apple", "banana", "cherry"}
	if len(items) != len(want) {
		t.Fatalf("Items = %v, want %v", items, want)
	}
	for i := range want {
		if items[i] != want[i] {
			t.Fatalf("Items = %v, want %v", items, want)
		}
	}

	empty := NewDefault()
	added, err = empty.AddLines(strings.NewReader(""))
	if err != nil {
		t.Fatalf("AddLines on empty input: %v", err)
	}
	if added != 0 || empty.Total() != 0 {
		t.Fatalf("empty input recorded %d occurrences, Total %d", added, empty.Total())
	}

	noTrailing := NewDefault()
	added, err = noTrailing.AddLines(strings.NewReader("one\ntwo"))
	if err != nil {
		t.Fatalf("AddLines: %v", err)
	}
	if added != 2 {
		t.Fatalf("a missing final newline must not drop the last item, recorded %d", added)
	}

	if _, err := s.AddLines(nil); err == nil {
		t.Fatal("AddLines(nil) must fail")
	}
}

func TestCMSCloneReset(t *testing.T) {
	s := NewDefault()
	addN(t, s, "keep", 30)
	addN(t, s, "also", 12)
	baseline := s.EstimateString("keep")
	baseTotal := s.Total()

	clone := s.Clone()
	if got := clone.EstimateString("keep"); got != baseline {
		t.Fatalf("clone Estimate = %d, want %d", got, baseline)
	}
	if got := clone.Total(); got != baseTotal {
		t.Fatalf("clone Total = %d, want %d", got, baseTotal)
	}
	if got, want := clone.Distinct(), s.Distinct(); got != want {
		t.Fatalf("clone Distinct = %d, want %d", got, want)
	}

	addN(t, clone, "keep", 5)
	addN(t, clone, "fresh", 3)
	if got := s.EstimateString("keep"); got != baseline {
		t.Fatalf("writing to the clone moved the original estimate to %d", got)
	}
	if got := s.Total(); got != baseTotal {
		t.Fatalf("writing to the clone moved the original Total to %d", got)
	}
	if s.Seen("fresh") {
		t.Fatal("writing to the clone must not add keys to the original")
	}

	row, err := s.Row(0)
	if err != nil {
		t.Fatalf("Row: %v", err)
	}
	for i := range row {
		row[i] = 12345
	}
	if got := s.EstimateString("keep"); got != baseline {
		t.Fatal("Row must return a copy the caller can scribble on")
	}

	s.Reset()
	if got := s.Total(); got != 0 {
		t.Fatalf("Total after Reset = %d, want 0", got)
	}
	if got := s.Distinct(); got != 0 {
		t.Fatalf("Distinct after Reset = %d, want 0", got)
	}
	if got := s.EstimateString("keep"); got != 0 {
		t.Fatalf("Estimate after Reset = %d, want 0", got)
	}
	if s.Seen("keep") {
		t.Fatal("Reset must clear the recorded keys")
	}
	if s.Depth() != DefaultDepth || s.Width() != DefaultWidth {
		t.Fatal("Reset must keep the matrix shape")
	}
	if load := s.Load(); load != 0 {
		t.Fatalf("Load after Reset = %v, want 0", load)
	}

	hitters, err := s.HeavyHitters(0)
	if err != nil {
		t.Fatalf("HeavyHitters: %v", err)
	}
	if len(hitters) != 0 {
		t.Fatalf("HeavyHitters after Reset returned %d entries", len(hitters))
	}
}
