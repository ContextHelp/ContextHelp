// Package registry — entity reconciliation for multi-registry environments.
//
// When two or more registries define the same entity slug, a merge strategy
// resolves which field values "win". Two strategies are supported:
//
//   - LastWriteWins (default): the most-recently-updated registry definition
//     overwrites all fields.
//   - TrustScore: the registry with the higher trust score wins per-field;
//     falls back to last-write-wins on ties.
//
// Every field that is overwritten by reconciliation generates a ProvenanceRecord
// so callers can inspect which registry supplied which value.
// Conflicts are collected into a ConflictReport returned alongside the merged
// entity.
package registry

import (
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// MergeStrategy controls how conflicting field values are resolved.
type MergeStrategy string

const (
	// MergeLastWriteWins: most recently updated registry wins all fields.
	MergeLastWriteWins MergeStrategy = "last-write-wins"
	// MergeTrustScore: registry with higher TrustScore wins per-field;
	// ties fall back to last-write-wins.
	MergeTrustScore MergeStrategy = "trust-score"
)

// ProvenanceRecord tracks which registry supplied the current value of one field.
type ProvenanceRecord struct {
	Field       string    `json:"field"`
	RegistryURL string    `json:"registry_url"`
	UpdatedAt   time.Time `json:"updated_at"`
	TrustScore  float64   `json:"trust_score"`
}

// EntityProvenance maps field names to their provenance records.
type EntityProvenance map[string]ProvenanceRecord

// ConflictReport summarises field-level conflicts for one entity slug.
type ConflictReport struct {
	Slug      string            `json:"slug"`
	Field     string            `json:"field"`
	Registries []RegistryValue  `json:"registries"`
	Winner    string            `json:"winner"` // registry URL that won
	Strategy  MergeStrategy     `json:"strategy"`
}

// RegistryValue is one candidate value from one registry.
type RegistryValue struct {
	RegistryURL string    `json:"registry_url"`
	Value       string    `json:"value"`
	UpdatedAt   time.Time `json:"updated_at"`
	TrustScore  float64   `json:"trust_score"`
}

// ReconcileResult is the output of a multi-registry entity reconciliation.
type ReconcileResult struct {
	// Merged is the final resolved entity.
	Merged *storage.Entity
	// Provenance maps each field to the registry that supplied it.
	Provenance EntityProvenance
	// Conflicts lists every field where registries disagreed.
	Conflicts []ConflictReport
}

// RegistryEntity pairs a fetched entity with its registry metadata.
type RegistryEntity struct {
	Entity      *storage.Entity
	RegistryURL string
	UpdatedAt   time.Time
	TrustScore  float64 // 0.0–1.0; 0 means unset (treated as low trust)
}

// Reconcile merges candidate entities from multiple registries into a single
// resolved entity, applying strategy. The local entity (from cache) is treated
// as origin priority = "local" and wins by default unless overridden.
//
// candidates must have at least one element. If only one candidate exists,
// Reconcile returns it as-is with empty provenance and no conflicts.
func Reconcile(slug string, candidates []RegistryEntity, strategy MergeStrategy) ReconcileResult {
	if len(candidates) == 0 {
		return ReconcileResult{}
	}
	if len(candidates) == 1 {
		prov := provenanceFromSingle(candidates[0])
		return ReconcileResult{
			Merged:     candidates[0].Entity,
			Provenance: prov,
		}
	}

	switch strategy {
	case MergeTrustScore:
		return reconcileTrustScore(slug, candidates)
	default:
		return reconcileLastWriteWins(slug, candidates)
	}
}

// reconcileLastWriteWins picks the candidate whose UpdatedAt is most recent
// as the "base" winner, then reports conflicts for any field that differs
// across registries.
func reconcileLastWriteWins(slug string, candidates []RegistryEntity) ReconcileResult {
	winner := candidates[0]
	for _, c := range candidates[1:] {
		if c.UpdatedAt.After(winner.UpdatedAt) {
			winner = c
		}
	}

	merged := cloneEntity(winner.Entity)
	prov := make(EntityProvenance)

	// Set provenance for every scalar field from the winner.
	setFieldProvenance(prov, winner)

	// Detect per-field conflicts.
	var conflicts []ConflictReport
	for _, field := range entityScalarFields() {
		values := collectValues(field, candidates)
		if len(uniqueValues(values)) > 1 {
			conflicts = append(conflicts, ConflictReport{
				Slug:       slug,
				Field:      field,
				Registries: values,
				Winner:     winner.RegistryURL,
				Strategy:   MergeLastWriteWins,
			})
		}
	}

	return ReconcileResult{
		Merged:     merged,
		Provenance: prov,
		Conflicts:  conflicts,
	}
}

// reconcileTrustScore picks the highest-trust registry as the winner.
// On exact trust-score ties, falls back to last-write-wins among tied candidates.
func reconcileTrustScore(slug string, candidates []RegistryEntity) ReconcileResult {
	winner := candidates[0]
	for _, c := range candidates[1:] {
		if c.TrustScore > winner.TrustScore {
			winner = c
		} else if c.TrustScore == winner.TrustScore && c.UpdatedAt.After(winner.UpdatedAt) {
			winner = c
		}
	}

	merged := cloneEntity(winner.Entity)
	prov := make(EntityProvenance)
	setFieldProvenance(prov, winner)

	var conflicts []ConflictReport
	for _, field := range entityScalarFields() {
		values := collectValues(field, candidates)
		if len(uniqueValues(values)) > 1 {
			conflicts = append(conflicts, ConflictReport{
				Slug:       slug,
				Field:      field,
				Registries: values,
				Winner:     winner.RegistryURL,
				Strategy:   MergeTrustScore,
			})
		}
	}

	return ReconcileResult{
		Merged:     merged,
		Provenance: prov,
		Conflicts:  conflicts,
	}
}

// OfflineResolution describes the result of resolving an entity while offline.
type OfflineResolution struct {
	Entity  *storage.Entity
	Cached  bool   // true when the local cache was used
	Warning string // non-empty when cache is stale or degraded
}

// ResolveOffline returns the cached entity when the registry is unreachable.
// If local is nil (no cache), it returns an error; callers should create a
// local stub entity instead.
func ResolveOffline(slug string, local *storage.Entity, registryErr error) (*OfflineResolution, error) {
	if local == nil {
		return nil, fmt.Errorf(
			"entity %q unavailable offline: not in local cache (registry error: %w)",
			slug, registryErr,
		)
	}
	staleness := ""
	if !local.UpdatedAt.IsZero() {
		age := time.Since(local.UpdatedAt)
		if age > 7*24*time.Hour {
			staleness = fmt.Sprintf("cached value is %s old; sync when online", age.Round(time.Hour))
		}
	}
	return &OfflineResolution{
		Entity:  local,
		Cached:  true,
		Warning: staleness,
	}, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// cloneEntity returns a shallow copy of e (slices/maps are re-aliased, not deep-copied).
func cloneEntity(e *storage.Entity) *storage.Entity {
	if e == nil {
		return nil
	}
	c := *e
	return &c
}

// entityScalarFields lists the string fields we track provenance on.
func entityScalarFields() []string {
	return []string{"title", "description", "namespace"}
}

// getField returns the string value of a named field from an entity.
func getField(e *storage.Entity, field string) string {
	switch field {
	case "title":
		return e.Title
	case "description":
		return e.Description
	case "namespace":
		return e.Namespace
	default:
		return ""
	}
}

// provenanceFromSingle builds provenance for a single-candidate result.
func provenanceFromSingle(c RegistryEntity) EntityProvenance {
	prov := make(EntityProvenance)
	for _, f := range entityScalarFields() {
		prov[f] = ProvenanceRecord{
			Field:       f,
			RegistryURL: c.RegistryURL,
			UpdatedAt:   c.UpdatedAt,
			TrustScore:  c.TrustScore,
		}
	}
	return prov
}

// setFieldProvenance records the winning registry for all scalar fields.
func setFieldProvenance(prov EntityProvenance, winner RegistryEntity) {
	for _, f := range entityScalarFields() {
		prov[f] = ProvenanceRecord{
			Field:       f,
			RegistryURL: winner.RegistryURL,
			UpdatedAt:   winner.UpdatedAt,
			TrustScore:  winner.TrustScore,
		}
	}
}

// collectValues gathers per-registry values for one field.
func collectValues(field string, candidates []RegistryEntity) []RegistryValue {
	out := make([]RegistryValue, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, RegistryValue{
			RegistryURL: c.RegistryURL,
			Value:       getField(c.Entity, field),
			UpdatedAt:   c.UpdatedAt,
			TrustScore:  c.TrustScore,
		})
	}
	return out
}

// uniqueValues returns distinct Value strings from a slice of RegistryValues.
func uniqueValues(vals []RegistryValue) []string {
	seen := make(map[string]bool, len(vals))
	var out []string
	for _, v := range vals {
		if !seen[v.Value] {
			seen[v.Value] = true
			out = append(out, v.Value)
		}
	}
	return out
}
