// Package feeds is the `feeds` protocol slot under internal/adapter/.
//
// Backends (rss today; future atom-only, jsonfeed) live as subpackages
// and identify themselves to the typed substrate via the Protocol
// constant defined here. The slot was introduced in Phase 2 by the
// 2026-05-06 amendment to ADR-065 to host fetch-only sensor adapters
// for syndicated content.
package feeds

// Protocol is the slot identity. Every Adapter under this slot returns
// this string from Adapter.Protocol(). The substrate's Registry uses
// this value to enforce one-platform-per-protocol per dPKMS instance
// (per ADR-065 §Decision; multi-platform composes via ADR-064
// federation, not multi-backend slots).
const Protocol = "feeds"
