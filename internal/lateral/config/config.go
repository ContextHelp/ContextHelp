package config

import "time"

// Weights are the eva-blended scorer weights (T08). yaml tags match
// the on-disk layout the user authors.
type Weights struct {
	SessionTopic     float64 `yaml:"session_topic"`
	CaptureWindow    float64 `yaml:"capture_window"`
	InterestRegistry float64 `yaml:"interest_registry"`
}

// Scoring bundles per-strategy thresholds and cap_k alongside weights.
type Scoring struct {
	Weights   Weights            `yaml:"weights"`
	Threshold map[string]float64 `yaml:"threshold"`
	CapK      map[string]int     `yaml:"cap_k"`
}

// Lifecycle holds the durations the reaper + promote/reject handlers
// consult. Days are integers because the on-disk file is human-edited.
type Lifecycle struct {
	ColdCycleDays        int `yaml:"cold_cycle_days"`
	SoftDeleteDays       int `yaml:"soft_delete_days"`
	P3ReferenceThreshold int `yaml:"p3_reference_threshold"`
}

// Jobs configures the kit job engine wiring (T13). EngineKind picks
// "memory" or "sqlite"; SqlitePath is consulted only when the sqlite
// engine is selected.
type Jobs struct {
	EngineKind string `yaml:"engine_kind"`
	SqlitePath string `yaml:"sqlite_path"`
}

// Config is the typed shape kit/core/config.Load fills.
type Config struct {
	Scoring   Scoring   `yaml:"scoring"`
	Lifecycle Lifecycle `yaml:"lifecycle"`
	Jobs      Jobs      `yaml:"jobs"`
}

// Defaults seeds Load before any layer is read. Mirrors the audited
// values from the substrate spec.
func Defaults() Config {
	return Config{
		Scoring: Scoring{
			Weights:   Weights{SessionTopic: 0.5, CaptureWindow: 0.3, InterestRegistry: 0.2},
			Threshold: map[string]float64{"sibling_repo": 0.45, "pinned_repo": 0.40, "starred_repo": 0.55},
			CapK:      map[string]int{"sibling_repo": 5, "pinned_repo": 3, "starred_repo": 3},
		},
		Lifecycle: Lifecycle{ColdCycleDays: 30, SoftDeleteDays: 30, P3ReferenceThreshold: 3},
		Jobs:      Jobs{EngineKind: "memory"},
	}
}

// SoftDelete returns the resurrection window as a duration.
func (l Lifecycle) SoftDelete() time.Duration {
	return time.Duration(l.SoftDeleteDays) * 24 * time.Hour
}
