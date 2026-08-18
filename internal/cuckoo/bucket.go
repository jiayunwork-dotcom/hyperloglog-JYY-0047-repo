package cuckoo

// BucketInfo holds diagnostic info about a single bucket.
type BucketInfo struct {
	Index    int
	Occupied int
	Empty    int
}

// BucketStats returns occupancy info for all buckets.
func (f *Filter) BucketStats() []BucketInfo {
	stats := make([]BucketInfo, len(f.buckets))
	for i, b := range f.buckets {
		info := BucketInfo{Index: i}
		for _, v := range b {
			if v != 0 {
				info.Occupied++
			} else {
				info.Empty++
			}
		}
		stats[i] = info
	}
	return stats
}

// EmptyBuckets returns the count of completely empty buckets.
func (f *Filter) EmptyBuckets() int {
	count := 0
	for _, b := range f.buckets {
		empty := true
		for _, v := range b {
			if v != 0 {
				empty = false
				break
			}
		}
		if empty {
			count++
		}
	}
	return count
}

// FullBuckets returns the count of completely full buckets.
func (f *Filter) FullBuckets() int {
	count := 0
	for _, b := range f.buckets {
		full := true
		for _, v := range b {
			if v == 0 {
				full = false
				break
			}
		}
		if full {
			count++
		}
	}
	return count
}

// Capacity returns the total number of slots.
func (f *Filter) Capacity() int {
	return int(f.numBuckets) * bucketSize
}
