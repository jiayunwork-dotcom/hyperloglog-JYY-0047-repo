// Package topk implements a probabilistic top-K frequent elements tracker
// using a count-min sketch and a min-heap. It maintains an approximate ranking
// of the most frequent elements seen in a stream.
package topk

import (
	"container/heap"
	"hash/fnv"
	"sort"
	"sync"
)

// Item represents an element with its estimated frequency.
type Item struct {
	Key   string
	Count uint64
}

// Tracker maintains the top-K elements.
type Tracker struct {
	mu   sync.Mutex
	k    int
	cms  [][]uint32 // count-min sketch
	rows int
	cols int
	heap minHeap
	set  map[string]int // key -> index in heap
}

// New creates a tracker for the top k elements with d rows and w columns.
func New(k, d, w int) *Tracker {
	if k <= 0 {
		k = 10
	}
	if d <= 0 {
		d = 4
	}
	if w <= 0 {
		w = 1024
	}
	cms := make([][]uint32, d)
	for i := range cms {
		cms[i] = make([]uint32, w)
	}
	return &Tracker{
		k:    k,
		cms:  cms,
		rows: d,
		cols: w,
		set:  make(map[string]int),
	}
}

// Add observes an element and updates the top-K.
func (t *Tracker) Add(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Update CMS.
	est := t.addToCMS(key)

	// Update heap.
	if idx, ok := t.set[key]; ok {
		t.heap[idx].Count = est
		heap.Fix(&t.heap, idx)
	} else if len(t.heap) < t.k {
		item := &heapItem{Key: key, Count: est}
		heap.Push(&t.heap, item)
		t.set[key] = len(t.heap) - 1
	} else if est > t.heap[0].Count {
		old := t.heap[0].Key
		delete(t.set, old)
		t.heap[0] = &heapItem{Key: key, Count: est}
		heap.Fix(&t.heap, 0)
		t.set[key] = 0
	}
	// Re-index.
	for i, item := range t.heap {
		t.set[item.Key] = i
	}
}

// Top returns the current top-K items sorted by count descending.
func (t *Tracker) Top() []Item {
	t.mu.Lock()
	defer t.mu.Unlock()
	items := make([]Item, len(t.heap))
	for i, h := range t.heap {
		items[i] = Item{Key: h.Key, Count: h.Count}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Count > items[j].Count
	})
	return items
}

// Estimate returns the estimated count for a key.
func (t *Tracker) Estimate(key string) uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.estimate(key)
}

// Len returns the number of items in the top-K.
func (t *Tracker) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.heap)
}

// Reset clears all state.
func (t *Tracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range t.cms {
		for j := range t.cms[i] {
			t.cms[i][j] = 0
		}
	}
	t.heap = nil
	t.set = make(map[string]int)
}

func (t *Tracker) addToCMS(key string) uint64 {
	var min uint64 = ^uint64(0)
	for i := 0; i < t.rows; i++ {
		col := t.hash(key, i)
		t.cms[i][col]++
		if uint64(t.cms[i][col]) < min {
			min = uint64(t.cms[i][col])
		}
	}
	return min
}

func (t *Tracker) estimate(key string) uint64 {
	var min uint64 = ^uint64(0)
	for i := 0; i < t.rows; i++ {
		col := t.hash(key, i)
		if uint64(t.cms[i][col]) < min {
			min = uint64(t.cms[i][col])
		}
	}
	return min
}

func (t *Tracker) hash(key string, seed int) int {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	_, _ = h.Write([]byte{byte(seed)})
	return int(h.Sum64() % uint64(t.cols))
}

// --- min-heap implementation ---

type heapItem struct {
	Key   string
	Count uint64
}

type minHeap []*heapItem

func (h minHeap) Len() int            { return len(h) }
func (h minHeap) Less(i, j int) bool   { return h[i].Count < h[j].Count }
func (h minHeap) Swap(i, j int)        { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x interface{})  { *h = append(*h, x.(*heapItem)) }
func (h *minHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}
