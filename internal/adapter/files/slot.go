// Package files is the `files` protocol slot under internal/adapter/.
//
// Backends (local-fs Phase 2; future s3, sshfs/sftp) live as
// subpackages and identify themselves to the typed substrate via the
// Protocol constant defined here. Per ADR-065 §Amendment 2026-05-06
// the files slot was pulled forward from Phase 4 to host filesystem
// sensors (local-fs, s3, sshfs).
package files

// Protocol is the slot identity. Every Adapter under this slot returns
// this string from Adapter.Protocol(). The substrate's Registry uses
// this value to enforce one-platform-per-protocol per dPKMS instance
// (per ADR-065 §Decision; multi-platform composes via ADR-064
// federation, not multi-backend slots).
const Protocol = "files"
