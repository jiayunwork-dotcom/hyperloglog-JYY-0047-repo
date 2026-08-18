package topk

import (
	"fmt"
	"testing"
)

func TestBasicTopK(t *testing.T) {
	tk := New(3, 4, 256)
	for i := 0; i < 100; i++ {
		tk.Add("a")
	}
	for i := 0; i < 50; i++ {
		tk.Add("b")
	}
	for i := 0; i < 10; i++ {
		tk.Add("c")
	}
	for i := 0; i < 5; i++ {
		tk.Add(fmt.Sprintf("rare_%d", i))
	}

	top := tk.Top()
	if len(top) != 3 {
		t.Fatalf("expected 3, got %d", len(top))
	}
	if top[0].Key != "a" {
		t.Fatalf("expected 'a' at top, got %q", top[0].Key)
	}
}

func TestEstimate(t *testing.T) {
	tk := New(5, 4, 256)
	for i := 0; i < 42; i++ {
		tk.Add("target")
	}
	est := tk.Estimate("target")
	if est < 40 || est > 44 {
		t.Fatalf("estimate off: %d", est)
	}
}

func TestReset(t *testing.T) {
	tk := New(5, 4, 256)
	tk.Add("x")
	tk.Reset()
	if tk.Len() != 0 {
		t.Fatal("expected empty after reset")
	}
}
