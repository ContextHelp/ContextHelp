// Package watchedfs is the `watched-fs` protocol slot under
// internal/adapter/.
//
// Unlike `files.local-fs` (a scan-on-tick sensor that returns the full
// tree state at each Fetch), watched-fs is an **fsnotify event stream**
// sensor that returns deltas — files modified or created since the
// previous Fetch. Per ADR-065 §Amendment 2026-05-06 + the
// policy/ambient.yaml catalog in docs/ctxt/ambient.md, watched-fs is a
// distinct slot, not a child of `files`. The two surfaces serve
// different operator intents and the substrate keeps them separate.
package watchedfs

// Protocol is the slot identity. Every Adapter under this slot returns
// this string from Adapter.Protocol(). The substrate's Registry uses
// this value to enforce one-platform-per-protocol per dPKMS instance
// (per ADR-065 §Decision; multi-platform composes via ADR-064
// federation, not multi-backend slots).
const Protocol = "watched-fs"
