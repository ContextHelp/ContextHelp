package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// Signature statuses of the entity index a pending entity came in.
const (
	// SignatureVerified: the registry declares a public key and the
	// index's Ed25519 signature checked out.
	SignatureVerified = "verified"
	// SignatureUnsigned: the registry declares no public key, so the
	// index was not verified.
	SignatureUnsigned = "unsigned"
)

// PendingEntity is an entity an untrusted registry served, held in the
// approval queue (registry_pending_entities) instead of being written.
// One row per (RegistryName, Slug, Version); syncing the registry again
// refreshes LastSeenAt and drops the versions it no longer serves.
type PendingEntity struct {
	// ID is PendingEntityID(RegistryName, Slug, Version): stable across
	// syncs, so the same held entity keeps its ID.
	ID           string `json:"id"`
	RegistryName string `json:"registry"`
	RegistryURL  string `json:"registry_url"`
	Slug         string `json:"slug"`
	Namespace    string `json:"namespace,omitempty"`
	Title        string `json:"title,omitempty"`
	// Version is the entity's version_hash, or "sha256:<hex>" of the
	// payload when the registry serves none (PendingVersion).
	Version string `json:"version"`
	// Entity is the payload: what approval writes.
	Entity *Entity `json:"entity"`
	// SignatureStatus is SignatureVerified or SignatureUnsigned.
	SignatureStatus string    `json:"signature_status"`
	FirstSeenAt     time.Time `json:"first_seen_at"`
	LastSeenAt      time.Time `json:"last_seen_at"`
}

// PendingEntityFilter narrows a pending entity listing.
type PendingEntityFilter struct {
	// RegistryName keeps one registry's entities; "" keeps all.
	RegistryName string
}

// PendingEntityStore is the persistent approval queue for entities
// untrusted registries served.
type PendingEntityStore interface {
	// Upsert queues p, keyed by p.ID. A row already queued keeps its
	// FirstSeenAt and takes everything else from p. Rows for the same
	// registry and slug at another version are removed: the registry
	// no longer serves them.
	Upsert(ctx context.Context, p *PendingEntity) error
	// List returns the queued entities matching filter, ordered by
	// registry, then slug.
	List(ctx context.Context, filter PendingEntityFilter) ([]*PendingEntity, error)
	// Get returns one queued entity; ErrNotFound when id is not queued.
	Get(ctx context.Context, id string) (*PendingEntity, error)
	// Delete removes one queued entity; ErrNotFound when id is not
	// queued.
	Delete(ctx context.Context, id string) error
	// DeleteEntity removes every queued version of slug from
	// registryName (the entity was written by another route) and
	// returns how many rows went.
	DeleteEntity(ctx context.Context, registryName, slug string) (int, error)
}

// PendingVersion is the version that keys e in the approval queue: its
// version_hash, or a hash of its content when it has none.
func PendingVersion(e *Entity) string {
	if e.VersionHash != "" {
		return e.VersionHash
	}
	b, _ := json.Marshal(struct {
		Slug, Title, Description, Namespace string
		Aliases                             []string
		Metadata                            map[string]any
	}{e.Slug, e.Title, e.Description, e.Namespace, e.Aliases, e.Metadata})
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// PendingEntityID is the queue ID of slug at version from registry: the
// first 16 hex digits of a SHA-256 over the three.
func PendingEntityID(registryName, slug, version string) string {
	sum := sha256.Sum256([]byte(registryName + "\x00" + slug + "\x00" + version))
	return hex.EncodeToString(sum[:8])
}
