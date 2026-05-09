// Package observe surfaces composite observability signals for the
// lateral discovery substrate. Distinct from internal/lateral/events
// (raw bus topics) and internal/lateral/eval (offline replay) — observe
// computes derived metrics that consume both.
//
// quality.go ships the per-strategy quality score: a weighted average
// of offline precision (from labelled fixtures) and production
// conversion rate (from the candidate→promoted bus telemetry). It's
// the headline number the Grafana dashboard's per-strategy bargauge
// panel renders, and the input the per-strategy review checkpoints
// (30d/90d, see docs/lateral/launch-summary.md) measure progress
// against.
package observe
