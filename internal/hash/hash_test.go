package hash

import (
	"errors"
	"math/bits"
	"testing"
)

func TestHashDeterministic(t *testing.T) {
	inputs := []string{
		"",
		"a",
		"b",
		"ab",
		"ba",
		"hyperloglog",
		"the quick brown fox jumps over the lazy dog",
	}

	first := make(map[string]uint64, len(inputs))
	for _, in := range inputs {
		first[in] = Hash64([]byte(in))
	}

	for round := 0; round < 4; round++ {
		for _, in := range inputs {
			got := Hash64([]byte(in))
			if got != first[in] {
				t.Fatalf("Hash64(%q) round %d = %#x, want %#x", in, round, got, first[in])
			}
			if s := Hash64String(in); s != got {
				t.Fatalf("Hash64String(%q) = %#x, Hash64 = %#x", in, s, got)
			}
		}
	}

	if Hash64(nil) != Hash64([]byte{}) {
		t.Fatal("nil and empty slice must hash alike")
	}

	seen := make(map[uint64]string, len(inputs))
	for _, in := range inputs {
		h := first[in]
		if prev, dup := seen[h]; dup {
			t.Fatalf("digest collision between %q and %q at %#x", prev, in, h)
		}
		seen[h] = in
	}

	if Hash64([]byte("ab")) == Hash64([]byte("ba")) {
		t.Fatal("byte order must affect the digest")
	}
}

func TestHashStreamingMatchesOneShot(t *testing.T) {
	payload := []byte("cardinality-estimation-over-a-byte-stream")

	for _, chunk := range []int{1, 2, 3, 7, 16, len(payload)} {
		d := NewDigest()
		for off := 0; off < len(payload); off += chunk {
			end := off + chunk
			if end > len(payload) {
				end = len(payload)
			}
			n, err := d.Write(payload[off:end])
			if err != nil {
				t.Fatalf("chunk %d: Write: %v", chunk, err)
			}
			if n != end-off {
				t.Fatalf("chunk %d: Write returned %d, want %d", chunk, n, end-off)
			}
		}
		if got, want := d.Sum64(), Hash64(payload); got != want {
			t.Fatalf("chunk %d: Sum64 = %#x, want %#x", chunk, got, want)
		}
		if got, want := d.Len(), int64(len(payload)); got != want {
			t.Fatalf("chunk %d: Len = %d, want %d", chunk, got, want)
		}
		if again := d.Sum64(); again != Hash64(payload) {
			t.Fatalf("chunk %d: Sum64 is not repeatable", chunk)
		}
	}

	d := NewDigest()
	if _, err := d.WriteString("mixed"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := d.WriteByte('!'); err != nil {
		t.Fatalf("WriteByte: %v", err)
	}
	if got, want := d.Sum64(), Hash64([]byte("mixed!")); got != want {
		t.Fatalf("mixed writes = %#x, want %#x", got, want)
	}

	d.Reset()
	if got, want := d.Sum64(), Hash64(nil); got != want {
		t.Fatalf("after Reset Sum64 = %#x, want %#x", got, want)
	}
	if d.Len() != 0 {
		t.Fatalf("after Reset Len = %d, want 0", d.Len())
	}
	if got := d.String(); len(got) != 16 {
		t.Fatalf("String() = %q, want 16 hex digits", got)
	}
}

func TestHashSplitRange(t *testing.T) {
	for p := MinPrecision; p <= MaxPrecision; p++ {
		m, err := RegisterCount(p)
		if err != nil {
			t.Fatalf("p=%d: RegisterCount: %v", p, err)
		}
		if m != uint32(1)<<p {
			t.Fatalf("p=%d: RegisterCount = %d, want %d", p, m, uint32(1)<<p)
		}
		maxRho, err := MaxRho(p)
		if err != nil {
			t.Fatalf("p=%d: MaxRho: %v", p, err)
		}

		for i := 0; i < 4096; i++ {
			h := Mix64(uint64(i) * goldenGamma)
			idx, rho, err := Split(h, p)
			if err != nil {
				t.Fatalf("p=%d i=%d: Split: %v", p, i, err)
			}
			if idx >= m {
				t.Fatalf("p=%d i=%d: index %d out of range [0,%d)", p, i, idx, m)
			}
			if rho < 1 || rho > maxRho {
				t.Fatalf("p=%d i=%d: rho %d out of range [1,%d]", p, i, rho, maxRho)
			}
			gotIdx, err := Index(h, p)
			if err != nil {
				t.Fatalf("p=%d: Index: %v", p, err)
			}
			gotRho, err := Rho(h, p)
			if err != nil {
				t.Fatalf("p=%d: Rho: %v", p, err)
			}
			if gotIdx != idx || gotRho != rho {
				t.Fatalf("p=%d i=%d: Index/Rho = (%d,%d), Split = (%d,%d)", p, i, gotIdx, gotRho, idx, rho)
			}
			if !EncodableIndex(idx) {
				t.Fatalf("p=%d: index %d must be encodable", p, idx)
			}
			backIdx, backRho := Decode(Encode(idx, rho))
			if backIdx != idx || backRho != rho {
				t.Fatalf("p=%d: Encode/Decode round trip gave (%d,%d), want (%d,%d)", p, backIdx, backRho, idx, rho)
			}
		}
	}
}

func TestHashSplitBoundaries(t *testing.T) {
	const p = 14

	idx, rho, err := Split(0, p)
	if err != nil {
		t.Fatalf("Split(0): %v", err)
	}
	maxRho, err := MaxRho(p)
	if err != nil {
		t.Fatalf("MaxRho: %v", err)
	}
	if idx != 0 {
		t.Fatalf("Split(0) index = %d, want 0", idx)
	}
	if rho != maxRho {
		t.Fatalf("Split(0) rho = %d, want saturated %d", rho, maxRho)
	}

	allOnes := ^uint64(0)
	idx, rho, err = Split(allOnes, p)
	if err != nil {
		t.Fatalf("Split(^0): %v", err)
	}
	if idx != uint32(1)<<p-1 {
		t.Fatalf("Split(^0) index = %d, want %d", idx, uint32(1)<<p-1)
	}
	if rho != 1 {
		t.Fatalf("Split(^0) rho = %d, want 1", rho)
	}

	for shift := uint(0); shift < 64-p; shift++ {
		h := uint64(1) << shift
		_, rho, err := Split(h, p)
		if err != nil {
			t.Fatalf("shift %d: Split: %v", shift, err)
		}
		want := uint8(64-p-shift)
		if rho != want {
			t.Fatalf("shift %d: rho = %d, want %d", shift, rho, want)
		}
	}

	if got := PopCount(allOnes); got != 64 {
		t.Fatalf("PopCount(^0) = %d, want 64", got)
	}
	if got := PopCount(0); got != 0 {
		t.Fatalf("PopCount(0) = %d, want 0", got)
	}
	if bits.OnesCount64(goldenGamma) != PopCount(goldenGamma) {
		t.Fatal("PopCount must agree with math/bits")
	}
}

func TestHashPrecisionErrors(t *testing.T) {
	bad := []uint{0, 1, 2, 3, 19, 20, 64, 1000}
	for _, p := range bad {
		if err := ValidatePrecision(p); err == nil {
			t.Fatalf("ValidatePrecision(%d) = nil, want error", p)
		} else if !errors.Is(err, ErrPrecisionRange) {
			t.Fatalf("ValidatePrecision(%d) = %v, want ErrPrecisionRange", p, err)
		}
		if _, _, err := Split(1, p); !errors.Is(err, ErrPrecisionRange) {
			t.Fatalf("Split precision %d = %v, want ErrPrecisionRange", p, err)
		}
		if _, err := Index(1, p); !errors.Is(err, ErrPrecisionRange) {
			t.Fatalf("Index precision %d = %v, want ErrPrecisionRange", p, err)
		}
		if _, err := Rho(1, p); !errors.Is(err, ErrPrecisionRange) {
			t.Fatalf("Rho precision %d = %v, want ErrPrecisionRange", p, err)
		}
		if _, err := RegisterCount(p); !errors.Is(err, ErrPrecisionRange) {
			t.Fatalf("RegisterCount precision %d = %v, want ErrPrecisionRange", p, err)
		}
		if _, err := MaxRho(p); !errors.Is(err, ErrPrecisionRange) {
			t.Fatalf("MaxRho precision %d = %v, want ErrPrecisionRange", p, err)
		}
	}

	for p := MinPrecision; p <= MaxPrecision; p++ {
		if err := ValidatePrecision(p); err != nil {
			t.Fatalf("ValidatePrecision(%d) = %v, want nil", p, err)
		}
	}

	var pe *PrecisionError
	err := ValidatePrecision(0)
	if !errors.As(err, &pe) {
		t.Fatalf("ValidatePrecision(0) = %T, want *PrecisionError", err)
	}
	if pe.Precision != 0 {
		t.Fatalf("PrecisionError.Precision = %d, want 0", pe.Precision)
	}
	if pe.Error() == "" {
		t.Fatal("PrecisionError.Error must not be empty")
	}
}

func TestHashSeedIndependence(t *testing.T) {
	payload := []byte("seed-independence")
	base := Hash64(payload)

	seen := map[uint64]int{base: -1}
	for i := 0; i < 64; i++ {
		seed := DeriveSeed(i)
		h := Seeded(payload, seed)
		if prev, dup := seen[h]; dup {
			t.Fatalf("seed %d collides with seed index %d", i, prev)
		}
		seen[h] = i
	}

	seeds := make(map[uint64]int, 256)
	for i := 0; i < 256; i++ {
		s := DeriveSeed(i)
		if prev, dup := seeds[s]; dup {
			t.Fatalf("DeriveSeed(%d) == DeriveSeed(%d)", i, prev)
		}
		seeds[s] = i
	}
	if DeriveSeed(7) != DeriveSeed(7) {
		t.Fatal("DeriveSeed must be deterministic")
	}
}

func TestHashAvalanche(t *testing.T) {
	const trials = 512
	flips := 0
	total := 0

	for i := 0; i < trials; i++ {
		base := Mix64(uint64(i) * goldenGamma)
		for bit := uint(0); bit < 64; bit++ {
			other := Mix64((uint64(i) * goldenGamma) ^ (uint64(1) << bit))
			flips += PopCount(base ^ other)
			total += 64
		}
	}

	ratio := float64(flips) / float64(total)
	if ratio < 0.4 || ratio > 0.6 {
		t.Fatalf("bit flip ratio = %.4f, want within [0.40,0.60]", ratio)
	}

	if Hash64(nil) == 0 {
		t.Fatal("digest of empty input must not be zero")
	}
	inverse := make(map[uint64]uint64, 4096)
	for i := uint64(0); i < 4096; i++ {
		m := Mix64(i)
		if prev, dup := inverse[m]; dup {
			t.Fatalf("Mix64 collision: %d and %d", prev, i)
		}
		inverse[m] = i
	}
}

func TestHashIndexUniformity(t *testing.T) {
	const p = 8
	m, err := RegisterCount(p)
	if err != nil {
		t.Fatalf("RegisterCount: %v", err)
	}

	buckets := make([]int, m)
	const n = 200000
	for i := 0; i < n; i++ {
		h := Hash64String("uniform-" + itoa(uint(i)))
		idx, err := Index(h, p)
		if err != nil {
			t.Fatalf("Index: %v", err)
		}
		buckets[idx]++
	}

	expected := float64(n) / float64(m)
	for i, c := range buckets {
		if c == 0 {
			t.Fatalf("bucket %d never used", i)
		}
		dev := float64(c)/expected - 1
		if dev < -0.25 || dev > 0.25 {
			t.Fatalf("bucket %d count %d deviates %.3f from %.1f", i, c, dev, expected)
		}
	}
}
