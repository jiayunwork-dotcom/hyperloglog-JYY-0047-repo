package hll

import (
	"bufio"
	"io"
	"strings"
)

// AddAll adds multiple string items to the HLL.
func (h *HLL) AddAll(items []string) int {
	count := 0
	for _, item := range items {
		if err := h.AddString(item); err == nil {
			count++
		}
	}
	return count
}

// AddReader reads lines from r and adds each as an item.
func (h *HLL) AddReader(r io.Reader) (int, error) {
	scanner := bufio.NewScanner(r)
	count := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := h.AddString(line); err != nil {
			return count, err
		}
		count++
	}
	return count, scanner.Err()
}

// CountUnique creates a new HLL and counts unique items from a slice.
func CountUnique(items []string) uint64 {
	h := NewDefault()
	for _, item := range items {
		_ = h.AddString(item)
	}
	return h.Count()
}

// CountUniqueBytes creates a new HLL and counts unique byte slices.
func CountUniqueBytes(items [][]byte) uint64 {
	h := NewDefault()
	for _, item := range items {
		_ = h.AddBytes(item)
	}
	return h.Count()
}
