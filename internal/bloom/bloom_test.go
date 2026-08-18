package bloom

import (
	"bytes"
	"fmt"
	"testing"
)

func TestAddAndContains(t *testing.T) {
	f := New(100, 0.01)
	f.Add([]byte("hello"))
	if !f.Contains([]byte("hello")) {
		t.Fatal("expected contains")
	}
}

func TestFalsePositiveRate(t *testing.T) {
	f := New(1000, 0.01)
	for i := 0; i < 1000; i++ {
		f.Add([]byte(fmt.Sprintf("key_%d", i)))
	}
	fp := 0
	for i := 1000; i < 2000; i++ {
		if f.Contains([]byte(fmt.Sprintf("key_%d", i))) {
			fp++
		}
	}
	if fp > 50 {
		t.Fatalf("too many false positives: %d/1000", fp)
	}
}

func TestReset(t *testing.T) {
	f := New(100, 0.01)
	f.Add([]byte("x"))
	f.Reset()
	if f.Contains([]byte("x")) {
		t.Fatal("should not contain after reset")
	}
}

func TestSerialize(t *testing.T) {
	f := New(100, 0.01)
	for i := 0; i < 50; i++ {
		f.Add([]byte(fmt.Sprintf("item_%d", i)))
	}
	var buf bytes.Buffer
	f.WriteTo(&buf)

	f2 := &Filter{}
	f2.ReadFrom(&buf)
	for i := 0; i < 50; i++ {
		if !f2.Contains([]byte(fmt.Sprintf("item_%d", i))) {
			t.Fatalf("missing item_%d after deserialize", i)
		}
	}
}

func TestFillRatio(t *testing.T) {
	f := New(10, 0.5)
	if f.FillRatio() != 0 {
		t.Fatal("empty filter should have 0 fill")
	}
	f.Add([]byte("a"))
	if f.FillRatio() <= 0 {
		t.Fatal("should be positive after add")
	}
}
