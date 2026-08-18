package hll

import (
	"math"

	"hyperloglog/internal/hash"
)

// linearCountingCutoff is the multiple of m below which the raw HyperLogLog
// estimator is replaced by linear counting.
//
// The raw harmonic-mean estimator is badly biased when a large fraction of the
// registers are still empty. In that regime the number of empty registers is
// itself a good estimator, so the classic construction switches over at
// 2.5*m and only when at least one register is empty.
const linearCountingCutoff = 2.5

// Alpha returns the bias correction constant alpha_m for m registers.
//
// The small-m cases are the tabulated values from the original analysis; above
// 64 registers the closed form is accurate enough.
func Alpha(m uint32) float64 {
	switch m {
	case 16:
		return 0.673
	case 32:
		return 0.697
	case 64:
		return 0.709
	default:
		return 0.7213 / (1.0 + 1.079/float64(m))
	}
}

// StandardError returns the expected relative standard error at precision p,
// which is 1.04/sqrt(m).
func StandardError(p uint) (float64, error) {
	m, err := hash.RegisterCount(p)
	if err != nil {
		return 0, err
	}
	return 1.04 / math.Sqrt(float64(m)), nil
}

// LinearCounting estimates how many distinct buckets were hit given that
// zeros of m buckets are still empty.
//
// This is the classic balls-into-bins estimator m*ln(m/zeros). When no bucket
// is empty the estimator diverges, so the caller must not use it there; this
// function reports the saturating value m*ln(m) instead of an infinity.
func LinearCounting(m, zeros float64) float64 {
	if m <= 0 {
		return 0
	}
	if zeros <= 0 {
		return m * math.Log(m)
	}
	if zeros > m {
		zeros = m
	}
	return m * math.Log(m/zeros)
}

// RawEstimate applies the harmonic-mean estimator to a register array.
//
// Each register contributes 2^-value; the reciprocal of that sum is a harmonic
// mean, and alpha_m*m^2 rescales it into a cardinality.
func RawEstimate(registers []uint8) float64 {
	if len(registers) == 0 {
		return 0
	}
	m := float64(len(registers))
	sum := 0.0
	for _, v := range registers {
		sum += math.Ldexp(1, -int(v))
	}
	if sum == 0 {
		return 0
	}
	return Alpha(uint32(len(registers))) * m * m / sum
}

// CountZeros reports how many registers are still empty.
func CountZeros(registers []uint8) uint32 {
	var zeros uint32
	for _, v := range registers {
		if v == 0 {
			zeros++
		}
	}
	return zeros
}

// counters summarises the register array without materialising it.
//
// zeros is the number of empty registers and sum is the sum of 2^-value over
// all m registers. A sparse sketch reaches the same numbers as the dense one
// it would promote to, because every register it does not list is empty and an
// empty register contributes 2^0 = 1 to the sum.
func (h *HLL) counters() (zeros uint32, sum float64) {
	if h.sparse != nil {
		zeros = h.m - uint32(len(h.sparse))
		sum = float64(zeros)
		for _, rho := range h.sparse {
			sum += math.Ldexp(1, -int(rho))
		}
		return zeros, sum
	}
	for _, v := range h.dense {
		if v == 0 {
			zeros++
		}
		sum += math.Ldexp(1, -int(v))
	}
	return zeros, sum
}

// Count returns the estimated number of distinct elements folded into the
// sketch.
//
// An empty sketch estimates 0. Otherwise the harmonic-mean estimator is used,
// except in the small-cardinality regime - a raw estimate below 2.5*m with at
// least one register still empty - where linear counting over the empty
// registers is the better estimator.
//
// The estimate depends only on the register values, never on which
// representation currently holds them, so promoting a sketch from sparse to
// dense does not move its estimate.
func (h *HLL) Count() uint64 {
	zeros, sum := h.counters()
	if zeros == h.m {
		return 0
	}

	m := float64(h.m)
	raw := 0.0
	if sum > 0 {
		raw = Alpha(h.m) * m * m / sum
	}
	if raw <= linearCountingCutoff*m && zeros > 0 {
		return round(LinearCounting(m, float64(zeros)))
	}
	return round(raw)
}

// round converts a non-negative estimate to the nearest integer. Estimates are
// never negative, so half-up rounding is enough.
func round(v float64) uint64 {
	if v <= 0 {
		return 0
	}
	return uint64(math.Floor(v + 0.5))
}

// Stats is a diagnostic snapshot of a sketch. It is what the command line
// front end reports, and it is stable enough to assert against in tests.
type Stats struct {
	// Precision is p.
	Precision uint
	// Registers is m = 2^p.
	Registers uint32
	// Sparse reports which representation the sketch is in.
	Sparse bool
	// Occupied counts registers holding a non-zero value.
	Occupied uint32
	// Zeros counts registers still empty; Occupied+Zeros == Registers.
	Zeros uint32
	// MaxRegister is the largest value held by any register.
	MaxRegister uint8
	// RawEstimate is the harmonic-mean estimator before any small-range
	// correction. It is reported for diagnosis, not for use.
	RawEstimate float64
	// Estimate is the value Count returns.
	Estimate uint64
	// StandardError is the expected relative standard error at this
	// precision.
	StandardError float64
}

// Stats computes a diagnostic snapshot without modifying the sketch.
func (h *HLL) Stats() Stats {
	registers := h.Registers()
	zeros := CountZeros(registers)

	var maxRegister uint8
	for _, v := range registers {
		if v > maxRegister {
			maxRegister = v
		}
	}

	stdErr, err := StandardError(h.p)
	if err != nil {
		stdErr = 0
	}

	return Stats{
		Precision:     h.p,
		Registers:     h.m,
		Sparse:        h.sparse != nil,
		Occupied:      h.m - zeros,
		Zeros:         zeros,
		MaxRegister:   maxRegister,
		RawEstimate:   RawEstimate(registers),
		Estimate:      h.Count(),
		StandardError: stdErr,
	}
}

// RelativeError returns the signed relative error of the sketch's estimate
// against a known true cardinality. It is meant for calibration runs where the
// exact answer happens to be available.
func (h *HLL) RelativeError(trueCount uint64) float64 {
	if trueCount == 0 {
		if h.Count() == 0 {
			return 0
		}
		return math.Inf(1)
	}
	return float64(h.Count())/float64(trueCount) - 1
}
