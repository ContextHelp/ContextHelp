package pipeline

import (
	"fmt"
	"sort"
	"strings"
)

// CapabilitySet represents what the runtime environment can provide.
type CapabilitySet map[string]bool

// ValidateComposability checks that each step's Requires are satisfied
// by the SeedState plus all preceding steps' Produces.
func ValidateComposability(steps []PipelineStep) error {
	available := make(map[string]bool, len(SeedState))
	for _, k := range SeedState {
		available[k] = true
	}

	for i, step := range steps {
		c := step.Contract()
		for _, req := range c.Requires {
			canon := Canonicalize(req)
			if !available[canon] && !available[fieldPrefix(canon)] {
				return fmt.Errorf("step %d (%s): requires %q but not produced by preceding steps (available: %v)",
					i, step.Name(), req, sortedKeys(available))
			}
		}
		for _, prod := range c.Produces {
			available[Canonicalize(prod)] = true
		}
	}
	return nil
}

// ValidateCapabilities checks all steps' required capabilities against available ones.
// Returns indices of steps with unsatisfied capabilities.
func ValidateCapabilities(steps []PipelineStep, caps CapabilitySet) []int {
	var unsatisfied []int
	for i, step := range steps {
		for _, cap := range step.Contract().Capabilities {
			if !caps[cap] {
				unsatisfied = append(unsatisfied, i)
				break
			}
		}
	}
	return unsatisfied
}

// fieldPrefix returns the top-level field name from a dot-notation key.
// e.g., "Metadata.ocr_confidence" -> "Metadata"
func fieldPrefix(key string) string {
	if idx := strings.IndexByte(key, '.'); idx >= 0 {
		return key[:idx]
	}
	return ""
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// RemoveIndices returns a new slice with elements at the given indices removed.
func RemoveIndices(steps []PipelineStep, indices []int) []PipelineStep {
	remove := make(map[int]bool, len(indices))
	for _, idx := range indices {
		remove[idx] = true
	}
	result := make([]PipelineStep, 0, len(steps)-len(indices))
	for i, s := range steps {
		if !remove[i] {
			result = append(result, s)
		}
	}
	return result
}
