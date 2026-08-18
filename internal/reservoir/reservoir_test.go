package reservoir

import (
	"fmt"
	"testing"
)

func TestBasicSampling(t *testing.T) {
	s := New(10)
	for i := 0; i < 1000; i++ {
		s.Add(fmt.Sprintf("item_%d", i))
	}
	if s.Len() != 10 {
		t.Fatalf("expected 10, got %d", s.Len())
	}
	if s.Seen() != 1000 {
		t.Fatalf("expected 1000 seen, got %d", s.Seen())
	}
}

func TestUnderCapacity(t *testing.T) {
	s := New(100)
	for i := 0; i < 5; i++ {
		s.Add(fmt.Sprintf("x_%d", i))
	}
	if s.Len() != 5 {
		t.Fatalf("expected 5, got %d", s.Len())
	}
}

func TestReset(t *testing.T) {
	s := New(10)
	s.Add("hello")
	s.Reset()
	if s.Len() != 0 {
		t.Fatal("expected empty after reset")
	}
	if s.Seen() != 0 {
		t.Fatal("expected 0 seen after reset")
	}
}

func TestContains(t *testing.T) {
	s := New(10)
	s.Add("target")
	if !s.Contains("target") {
		t.Fatal("should contain target")
	}
}

func TestSamplingRate(t *testing.T) {
	s := New(10)
	for i := 0; i < 100; i++ {
		s.Add(fmt.Sprintf("i%d", i))
	}
	rate := s.SamplingRate()
	if rate < 0.09 || rate > 0.11 {
		t.Fatalf("expected ~0.1, got %f", rate)
	}
}
