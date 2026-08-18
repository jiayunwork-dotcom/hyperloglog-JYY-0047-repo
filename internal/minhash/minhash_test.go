package minhash

import (
	"fmt"
	"testing"
)

func TestSimilarityIdentical(t *testing.T) {
	a := New(64)
	b := New(64)
	for i := 0; i < 100; i++ {
		data := []byte(fmt.Sprintf("item_%d", i))
		a.Add(data)
		b.Add(data)
	}
	sim := Similarity(a, b)
	if sim < 0.9 {
		t.Fatalf("identical sets should have sim ~1.0, got %f", sim)
	}
}

func TestSimilarityDisjoint(t *testing.T) {
	a := New(64)
	b := New(64)
	for i := 0; i < 100; i++ {
		a.Add([]byte(fmt.Sprintf("a_%d", i)))
		b.Add([]byte(fmt.Sprintf("b_%d", i)))
	}
	sim := Similarity(a, b)
	if sim > 0.15 {
		t.Fatalf("disjoint sets should have low sim, got %f", sim)
	}
}

func TestMerge(t *testing.T) {
	a := New(64)
	b := New(64)
	a.Add([]byte("x"))
	b.Add([]byte("y"))
	m := Merge(a, b)
	if m == nil {
		t.Fatal("merge should not return nil")
	}
	if m.IsEmpty() {
		t.Fatal("merged should not be empty")
	}
}

func TestReset(t *testing.T) {
	s := New(32)
	s.Add([]byte("hello"))
	s.Reset()
	if !s.IsEmpty() {
		t.Fatal("should be empty after reset")
	}
}
