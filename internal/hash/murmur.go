package hash

// MurmurMix64 applies the murmur3 finalizer to a uint64.
// Useful for additional mixing when composing hash functions.
func MurmurMix64(k uint64) uint64 {
	k ^= k >> 33
	k *= 0xff51afd7ed558ccd
	k ^= k >> 33
	k *= 0xc4ceb9fe1a85ec53
	k ^= k >> 33
	return k
}

// MurmurMix32 applies the murmur3 32-bit finalizer.
func MurmurMix32(h uint32) uint32 {
	h ^= h >> 16
	h *= 0x85ebca6b
	h ^= h >> 13
	h *= 0xc2b2ae35
	h ^= h >> 16
	return h
}

// CombineHashes combines two hash values into one.
func CombineHashes(h1, h2 uint64) uint64 {
	h1 ^= h2 + 0x9e3779b97f4a7c15 + (h1 << 12) + (h1 >> 4)
	return h1
}

// FNV1aString computes FNV-1a hash of a string without allocation.
func FNV1aString(s string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}

// RotateLeft64 rotates x left by k bits.
func RotateLeft64(x uint64, k int) uint64 {
	return (x << uint(k)) | (x >> (64 - uint(k)))
}

// SplitMix64 is a simple splittable PRNG useful for seeding.
func SplitMix64(state *uint64) uint64 {
	*state += 0x9e3779b97f4a7c15
	z := *state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}
