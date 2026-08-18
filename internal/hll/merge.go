package hll

// MergeAll merges multiple HyperLogLog instances into one.
func MergeAll(hlls ...*HLL) *HLL {
	if len(hlls) == 0 {
		return nil
	}
	result := hlls[0].Clone()
	for _, h := range hlls[1:] {
		_ = result.Merge(h)
	}
	return result
}

// Intersection estimates the intersection cardinality using inclusion-exclusion:
// |A∩B| ≈ |A| + |B| - |A∪B|.
func Intersection(a, b *HLL) float64 {
	estA := float64(a.Count())
	estB := float64(b.Count())
	union := a.Clone()
	_ = union.Merge(b)
	estU := float64(union.Count())
	inter := estA + estB - estU
	if inter < 0 {
		return 0
	}
	return inter
}

// JaccardEstimate estimates the Jaccard similarity: J(A,B) = |A∩B| / |A∪B|.
func JaccardEstimate(a, b *HLL) float64 {
	union := a.Clone()
	_ = union.Merge(b)
	estU := float64(union.Count())
	if estU == 0 {
		return 0
	}
	inter := Intersection(a, b)
	return inter / estU
}
