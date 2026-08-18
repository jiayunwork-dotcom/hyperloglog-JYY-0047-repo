package cuckoo

import (
	"fmt"
	"testing"
)

func TestInsertAndContains(t *testing.T) {
	f := New(1000)
	for i := 0; i < 100; i++ {
		if err := f.Insert([]byte(fmt.Sprintf("key_%d", i))); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 100; i++ {
		if !f.Contains([]byte(fmt.Sprintf("key_%d", i))) {
			t.Fatalf("missing key_%d", i)
		}
	}
}

func TestDelete(t *testing.T) {
	f := New(100)
	f.Insert([]byte("hello"))
	if !f.Delete([]byte("hello")) {
		t.Fatal("expected delete to succeed")
	}
	if f.Contains([]byte("hello")) {
		t.Fatal("should not contain after delete")
	}
}

func TestDeleteMissing(t *testing.T) {
	f := New(100)
	if f.Delete([]byte("nope")) {
		t.Fatal("should not delete missing key")
	}
}

func TestCount(t *testing.T) {
	f := New(100)
	f.Insert([]byte("a"))
	f.Insert([]byte("b"))
	if f.Count() != 2 {
		t.Fatalf("expected 2, got %d", f.Count())
	}
}

func TestReset(t *testing.T) {
	f := New(100)
	f.Insert([]byte("x"))
	f.Reset()
	if f.Count() != 0 {
		t.Fatal("expected 0 after reset")
	}
	if f.Contains([]byte("x")) {
		t.Fatal("should not contain after reset")
	}
}

func TestLoadFactor(t *testing.T) {
	f := New(100)
	if f.LoadFactor() != 0 {
		t.Fatal("empty filter should have 0 load")
	}
	f.Insert([]byte("a"))
	if f.LoadFactor() <= 0 {
		t.Fatal("should be positive after insert")
	}
}
