// Package config provides configuration for the probabilistic data structures.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrInvalid is returned for invalid config values.
var ErrInvalid = errors.New("config: invalid")

// Config holds parameters for all probabilistic structures.
type Config struct {
	// HLL
	HLLPrecision int `json:"hll_precision"`

	// Bloom filter
	BloomExpectedN int     `json:"bloom_expected_n"`
	BloomFPRate    float64 `json:"bloom_fp_rate"`

	// Count-Min Sketch
	CMSRows int `json:"cms_rows"`
	CMSCols int `json:"cms_cols"`

	// Top-K
	TopK int `json:"top_k"`

	// Cuckoo
	CuckooCap int `json:"cuckoo_capacity"`

	// MinHash
	MinHashK int `json:"minhash_k"`
}

// Default returns sensible defaults.
func Default() Config {
	return Config{
		HLLPrecision:   14,
		BloomExpectedN: 100000,
		BloomFPRate:    0.01,
		CMSRows:        4,
		CMSCols:        2048,
		TopK:           100,
		CuckooCap:      10000,
		MinHashK:       128,
	}
}

// Validate checks field constraints.
func (c *Config) Validate() error {
	if c.HLLPrecision < 4 || c.HLLPrecision > 18 {
		return fmt.Errorf("%w: hll_precision must be in [4,18]", ErrInvalid)
	}
	if c.BloomExpectedN <= 0 {
		return fmt.Errorf("%w: bloom_expected_n must be positive", ErrInvalid)
	}
	if c.BloomFPRate <= 0 || c.BloomFPRate >= 1 {
		return fmt.Errorf("%w: bloom_fp_rate must be in (0,1)", ErrInvalid)
	}
	if c.CMSRows <= 0 || c.CMSCols <= 0 {
		return fmt.Errorf("%w: cms dimensions must be positive", ErrInvalid)
	}
	if c.TopK <= 0 {
		return fmt.Errorf("%w: top_k must be positive", ErrInvalid)
	}
	if c.CuckooCap <= 0 {
		return fmt.Errorf("%w: cuckoo_capacity must be positive", ErrInvalid)
	}
	if c.MinHashK <= 0 {
		return fmt.Errorf("%w: minhash_k must be positive", ErrInvalid)
	}
	return nil
}

// Save writes config to dir/config.json.
func (c *Config) Save(dir string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config.json"), data, 0o644)
}

// Load reads config from dir/config.json.
func Load(dir string) (Config, error) {
	path := filepath.Join(dir, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, err
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}
