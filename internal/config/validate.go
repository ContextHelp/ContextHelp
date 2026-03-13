package config

import "fmt"

// Validate checks that the Config is internally consistent.
// Called after Load() when strict validation is desired.
func (c *Config) Validate() error {
	if err := c.validateDuplicates(); err != nil {
		return err
	}
	return nil
}

func (c *Config) validateDuplicates() error {
	d := c.Duplicates
	policy := d.Policy
	if policy == "" {
		policy = "warn" // treat empty as default
	}
	switch policy {
	case "warn", "drop", "keep":
		// valid
	default:
		return fmt.Errorf("config: duplicates.policy must be one of \"warn\", \"drop\", \"keep\"; got %q", d.Policy)
	}
	if d.SimilarityThreshold < 0.0 || d.SimilarityThreshold > 1.0 {
		return fmt.Errorf("config: duplicates.similarity_threshold must be 0.0–1.0; got %f", d.SimilarityThreshold)
	}
	return nil
}
