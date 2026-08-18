package config

import (
	"testing"
)

func TestDefaultValid(t *testing.T) {
	c := Default()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejects(t *testing.T) {
	c := Default()
	c.HLLPrecision = 2
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for precision 2")
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	c := Default()
	c.TopK = 42
	if err := c.Save(dir); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TopK != 42 {
		t.Fatalf("expected 42, got %d", loaded.TopK)
	}
}

func TestLoadMissing(t *testing.T) {
	c, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if c.TopK != Default().TopK {
		t.Fatal("expected default")
	}
}
