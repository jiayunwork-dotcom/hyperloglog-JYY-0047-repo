// Package cms implements the Count-Min sketch, a sublinear structure for
// estimating how often each element occurred in a stream.
//
// A sketch is a d-by-w matrix of counters plus d independent hash functions.
// Adding an element increments one counter per row. Estimating an element reads
// the same d counters and returns the smallest: every counter that the element
// touches holds at least its true count, and collisions can only inflate a
// counter, so the minimum is the tightest available upper bound. Estimates are
// therefore never too low, and the excess falls as the matrix widens.
//
// A sketch alone cannot enumerate the elements it has seen, so this
// implementation also records the distinct element keys. That is what allows
// HeavyHitters to report the frequent elements rather than merely answer point
// queries about elements the caller already knows.
package cms

import (
	"bufio"
	"errors"
	"io"
	"math"
	"sort"
	"strings"

	"hyperloglog/internal/hash"
)

// Default matrix shape. At d=5, w=2048 the sketch occupies 80 KiB of counters
// and keeps the relative overshoot small for streams of a few thousand
// distinct elements.
const (
	DefaultDepth uint32 = 5
	DefaultWidth uint32 = 2048
)

// Matrix bounds. The depth ceiling reflects that accuracy gains from more rows
// are exponential in the confidence, so a handful of rows is always enough; the
// width ceiling keeps a single sketch under a gigabyte.
const (
	MinDepth uint32 = 1
	MaxDepth uint32 = 64
	MinWidth uint32 = 1
	MaxWidth uint32 = 1 << 22
)

// maxLineBytes bounds a single line read by AddLines.
const maxLineBytes = 1 << 20

// Sentinel errors returned by this package.
var (
	// ErrEmptyItem is returned when an element with no bytes is offered.
	ErrEmptyItem = errors.New("cms: empty item")
	// ErrNilSketch is returned when a nil sketch is passed to Merge.
	ErrNilSketch = errors.New("cms: nil sketch")
	// ErrDepthRange is returned for a depth outside [MinDepth, MaxDepth].
	ErrDepthRange = errors.New("cms: depth out of range")
	// ErrWidthRange is returned for a width outside [MinWidth, MaxWidth].
	ErrWidthRange = errors.New("cms: width out of range")
	// ErrDimensionMismatch is returned when merging sketches whose matrices
	// have different shapes; their counters are not comparable.
	ErrDimensionMismatch = errors.New("cms: dimension mismatch")
	// ErrCounterOverflow is returned when an increment would wrap a counter
	// or the running total.
	ErrCounterOverflow = errors.New("cms: counter overflow")
	// ErrPhiRange is returned when a heavy-hitter threshold falls outside
	// [0,1].
	ErrPhiRange = errors.New("cms: phi out of range")
	// ErrEpsilonRange is returned when a target error falls outside (0,1).
	ErrEpsilonRange = errors.New("cms: epsilon out of range")
	// ErrDeltaRange is returned when a target failure probability falls
	// outside (0,1).
	ErrDeltaRange = errors.New("cms: delta out of range")
)

// Sketch is a Count-Min sketch. It is not safe for concurrent use.
type Sketch struct {
	depth uint32
	width uint32
	rows  [][]uint64
	seeds []uint64
	total uint64
	keys  map[string]struct{}
}

// New returns an empty sketch with a depth-by-width counter matrix.
func New(depth, width uint32) (*Sketch, error) {
	if depth < MinDepth || depth > MaxDepth {
		return nil, ErrDepthRange
	}
	if width < MinWidth || width > MaxWidth {
		return nil, ErrWidthRange
	}

	rows := make([][]uint64, depth)
	seeds := make([]uint64, depth)
	for i := range rows {
		rows[i] = make([]uint64, width)
		seeds[i] = hash.DeriveSeed(i)
	}

	return &Sketch{
		depth: depth,
		width: width,
		rows:  rows,
		seeds: seeds,
		keys:  make(map[string]struct{}),
	}, nil
}

// NewDefault returns an empty sketch with the default matrix shape.
func NewDefault() *Sketch {
	s, err := New(DefaultDepth, DefaultWidth)
	if err != nil {
		panic("cms: default dimensions must be valid: " + err.Error())
	}
	return s
}

// NewWithAccuracy sizes a sketch from the accuracy a caller wants.
//
// epsilon is the tolerated overshoot as a fraction of the total count and
// delta is the probability of exceeding it. The standard sizing is
// w = ceil(e/epsilon) and d = ceil(ln(1/delta)).
func NewWithAccuracy(epsilon, delta float64) (*Sketch, error) {
	if !(epsilon > 0 && epsilon < 1) || math.IsNaN(epsilon) {
		return nil, ErrEpsilonRange
	}
	if !(delta > 0 && delta < 1) || math.IsNaN(delta) {
		return nil, ErrDeltaRange
	}

	width := uint64(math.Ceil(math.E / epsilon))
	depth := uint64(math.Ceil(math.Log(1 / delta)))
	if depth < uint64(MinDepth) {
		depth = uint64(MinDepth)
	}
	if depth > uint64(MaxDepth) {
		depth = uint64(MaxDepth)
	}
	if width > uint64(MaxWidth) {
		return nil, ErrWidthRange
	}
	if width < uint64(MinWidth) {
		width = uint64(MinWidth)
	}
	return New(uint32(depth), uint32(width))
}

// Depth returns d, the number of rows.
func (s *Sketch) Depth() uint32 { return s.depth }

// Width returns w, the number of columns per row.
func (s *Sketch) Width() uint32 { return s.width }

// Total returns the sum of all increments applied to the sketch.
func (s *Sketch) Total() uint64 { return s.total }

// Distinct returns how many distinct elements the sketch has recorded.
func (s *Sketch) Distinct() int { return len(s.keys) }

// Items returns the distinct elements in ascending order.
func (s *Sketch) Items() []string {
	out := make([]string, 0, len(s.keys))
	for k := range s.keys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// columns fills dst with the column this element occupies in each row.
//
// A single base digest is expanded into d columns by mixing it with the row
// seed. Because Mix64 is a bijection with good avalanche behaviour, the
// resulting columns behave like independent draws, which is what the Count-Min
// error bound assumes.
func (s *Sketch) columns(item []byte, dst []uint32) {
	base := hash.Hash64(item)
	for i := uint32(0); i < s.depth; i++ {
		mixed := hash.Mix64(base ^ s.seeds[i])
		dst[i] = uint32(mixed % uint64(s.width))
	}
}

// Add records one occurrence of item.
func (s *Sketch) Add(item []byte) error {
	return s.AddCount(item, 1)
}

// AddString is the string flavour of Add.
func (s *Sketch) AddString(item string) error {
	return s.AddCount([]byte(item), 1)
}

// AddCount records n occurrences of item.
//
// An item with no bytes is rejected: frequency inputs are identifiers, and an
// empty identifier is almost always a truncated read rather than a real
// element. A count of zero is accepted and changes nothing.
func (s *Sketch) AddCount(item []byte, n uint64) error {
	if len(item) == 0 {
		return ErrEmptyItem
	}
	if n == 0 {
		// Still record the key: the element was observed, it just carries
		// no weight yet.
		s.keys[string(item)] = struct{}{}
		return nil
	}
	if s.total > math.MaxUint64-n {
		return ErrCounterOverflow
	}

	cols := make([]uint32, s.depth)
	s.columns(item, cols)

	// Check every counter before touching any of them, so that a rejected
	// increment leaves the matrix exactly as it was.
	for i := uint32(0); i < s.depth; i++ {
		if s.rows[i][cols[i]] > math.MaxUint64-n {
			return ErrCounterOverflow
		}
	}
	for i := uint32(0); i < s.depth; i++ {
		s.rows[i][cols[i]] += n
	}

	s.total += n
	s.keys[string(item)] = struct{}{}
	return nil
}

// AddLines reads one element per line from r and records one occurrence of
// each.
//
// Surrounding whitespace is trimmed and lines that are empty after trimming
// are skipped rather than rejected. The number of occurrences recorded is
// returned, counting repeats.
func (s *Sketch) AddLines(r io.Reader) (int, error) {
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
		if err := s.AddString(item); err != nil {
			return added, err
		}
		added++
	}
	if err := sc.Err(); err != nil {
		return added, err
	}
	return added, nil
}

// Estimate returns the estimated number of occurrences of item.
//
// The result is the smallest of the d counters the element hashes to, which is
// an upper bound on the true count and never an undercount. An element with no
// bytes was never recorded, so it estimates 0.
func (s *Sketch) Estimate(item []byte) uint64 {
	if len(item) == 0 {
		return 0
	}
	cols := make([]uint32, s.depth)
	s.columns(item, cols)

	best := s.rows[0][cols[0]]
	for i := uint32(1); i < s.depth; i++ {
		if v := s.rows[i][cols[i]]; v < best {
			best = v
		}
	}
	return best
}

// EstimateString is the string flavour of Estimate.
func (s *Sketch) EstimateString(item string) uint64 {
	return s.Estimate([]byte(item))
}

// Seen reports whether the sketch recorded this element by name. Unlike
// Estimate it is exact, because the distinct keys are stored.
func (s *Sketch) Seen(item string) bool {
	_, ok := s.keys[item]
	return ok
}

// Merge folds other into s so that s estimates the combined stream.
//
// Counters add, because a counter's value is a sum of increments and the two
// sketches share the same hash functions. The result is identical to the
// sketch that would have come from feeding both streams into one sketch.
// other is not modified.
func (s *Sketch) Merge(other *Sketch) error {
	if other == nil {
		return ErrNilSketch
	}
	if other.depth != s.depth || other.width != s.width {
		return ErrDimensionMismatch
	}
	if s == other {
		return nil
	}
	if s.total > math.MaxUint64-other.total {
		return ErrCounterOverflow
	}

	// Validate the whole matrix first so that a rejected merge leaves the
	// receiver untouched.
	for i := uint32(0); i < s.depth; i++ {
		for j := uint32(0); j < s.width; j++ {
			if s.rows[i][j] > math.MaxUint64-other.rows[i][j] {
				return ErrCounterOverflow
			}
		}
	}
	for i := uint32(0); i < s.depth; i++ {
		for j := uint32(0); j < s.width; j++ {
			s.rows[i][j] += other.rows[i][j]
		}
	}

	s.total += other.total
	for k := range other.keys {
		s.keys[k] = struct{}{}
	}
	return nil
}

// Union returns a new sketch holding the combined stream of a and b, leaving
// both operands untouched.
func Union(a, b *Sketch) (*Sketch, error) {
	if a == nil || b == nil {
		return nil, ErrNilSketch
	}
	out := a.Clone()
	if err := out.Merge(b); err != nil {
		return nil, err
	}
	return out, nil
}

// Clone returns an independent deep copy of the sketch.
func (s *Sketch) Clone() *Sketch {
	out := &Sketch{
		depth: s.depth,
		width: s.width,
		total: s.total,
		rows:  make([][]uint64, s.depth),
		seeds: make([]uint64, s.depth),
		keys:  make(map[string]struct{}, len(s.keys)),
	}
	copy(out.seeds, s.seeds)
	for i := range s.rows {
		out.rows[i] = make([]uint64, s.width)
		copy(out.rows[i], s.rows[i])
	}
	for k := range s.keys {
		out.keys[k] = struct{}{}
	}
	return out
}

// Reset empties the counter matrix and the recorded keys, keeping the shape.
func (s *Sketch) Reset() {
	for i := range s.rows {
		for j := range s.rows[i] {
			s.rows[i][j] = 0
		}
	}
	s.total = 0
	s.keys = make(map[string]struct{})
}

// Row returns a copy of one counter row, for diagnosis.
func (s *Sketch) Row(i uint32) ([]uint64, error) {
	if i >= s.depth {
		return nil, ErrDimensionMismatch
	}
	out := make([]uint64, s.width)
	copy(out, s.rows[i])
	return out, nil
}

// Load reports the fraction of counters that are non-zero, averaged over the
// rows. A load close to 1 means the matrix is too narrow for the stream and
// estimates will overshoot badly.
func (s *Sketch) Load() float64 {
	if s.depth == 0 || s.width == 0 {
		return 0
	}
	used := 0
	for i := range s.rows {
		for _, v := range s.rows[i] {
			if v != 0 {
				used++
			}
		}
	}
	return float64(used) / float64(uint64(s.depth)*uint64(s.width))
}
