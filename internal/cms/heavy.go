package cms

import (
	"math"
	"sort"
)

// HeavyHitter is one frequent element together with its estimated count and
// the share of the stream that count represents.
type HeavyHitter struct {
	// Item is the element.
	Item string
	// Estimate is what Estimate reports for Item, an upper bound on its
	// true count.
	Estimate uint64
	// Share is Estimate divided by the stream total, in [0,1].
	Share float64
}

// HeavyHitters returns the elements whose estimated count exceeds phi times
// the stream total.
//
// The comparison is strict: an element sitting exactly on the threshold is not
// reported, so phi=0 returns every element with a non-zero estimate rather
// than every element ever seen. phi=1 can only return an element whose
// estimate exceeds the whole stream, which cannot happen, so it returns
// nothing.
//
// Results are ordered by descending estimate, ties broken by ascending item,
// so the ordering is total and does not depend on map iteration order. The
// result is always non-nil, even when empty.
func (s *Sketch) HeavyHitters(phi float64) ([]HeavyHitter, error) {
	if math.IsNaN(phi) || phi < 0 || phi > 1 {
		return nil, ErrPhiRange
	}

	out := make([]HeavyHitter, 0, 8)
	if s.total == 0 {
		return out, nil
	}

	total := float64(s.total)
	threshold := phi * total

	for item := range s.keys {
		estimate := s.EstimateString(item)
		if float64(estimate) <= threshold {
			continue
		}
		out = append(out, HeavyHitter{
			Item:     item,
			Estimate: estimate,
			Share:    float64(estimate) / total,
		})
	}

	sortHitters(out)
	return out, nil
}

// TopK returns the k elements with the largest estimates, ordered the same way
// HeavyHitters orders its results.
//
// Elements with a zero estimate are skipped. Fewer than k entries are returned
// when the sketch holds fewer elements than that.
func (s *Sketch) TopK(k int) ([]HeavyHitter, error) {
	if k < 0 {
		return nil, ErrPhiRange
	}
	out := make([]HeavyHitter, 0, k)
	if k == 0 || s.total == 0 {
		return out, nil
	}

	total := float64(s.total)
	all := make([]HeavyHitter, 0, len(s.keys))
	for item := range s.keys {
		estimate := s.EstimateString(item)
		if estimate == 0 {
			continue
		}
		all = append(all, HeavyHitter{
			Item:     item,
			Estimate: estimate,
			Share:    float64(estimate) / total,
		})
	}

	sortHitters(all)
	if len(all) > k {
		all = all[:k]
	}
	return append(out, all...), nil
}

// sortHitters imposes the canonical order: heaviest first, then by item so
// that equal estimates still come out in a fixed sequence.
func sortHitters(hitters []HeavyHitter) {
	sort.Slice(hitters, func(i, j int) bool {
		if hitters[i].Estimate != hitters[j].Estimate {
			return hitters[i].Estimate > hitters[j].Estimate
		}
		return hitters[i].Item < hitters[j].Item
	})
}

// ErrorBound returns the additive overshoot the sketch's shape guarantees for
// a single point query, that is e/w times the stream total.
//
// The true count of any element is at least Estimate minus this bound, with
// probability at least 1 - e^-d.
func (s *Sketch) ErrorBound() float64 {
	if s.width == 0 {
		return 0
	}
	return math.E / float64(s.width) * float64(s.total)
}

// Confidence returns the probability that a point query stays inside
// ErrorBound, that is 1 - e^-d.
func (s *Sketch) Confidence() float64 {
	return 1 - math.Exp(-float64(s.depth))
}
