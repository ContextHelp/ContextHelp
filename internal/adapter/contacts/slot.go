// Package contacts is the `contacts` protocol slot under
// internal/adapter/.
//
// Backends (cardamum CardDAV vdir today; future gcontacts /
// o365-contacts / icloud-contacts) live as subpackages and identify
// themselves to the typed substrate via the Protocol constant defined
// here.
package contacts

// Protocol is the slot identity. Every Adapter under this slot returns
// this string from Adapter.Protocol(). The substrate's Registry uses
// this value to enforce one-platform-per-protocol per dPKMS instance
// (per ADR-065 §Decision; multi-platform composes via ADR-064
// federation, not multi-backend slots).
const Protocol = "contacts"
