// Package email is the `email` protocol slot under internal/adapter/.
//
// Backends (himalaya, future stalwart, future mxhook+gmail / icloud /
// m365 / proton / fastmail) live as subpackages and identify themselves
// to the typed substrate via the Protocol constant defined here.
package email

// Protocol is the slot identity. Every Adapter under this slot returns
// this string from Adapter.Protocol(). The substrate's Registry uses
// this value to enforce one-platform-per-protocol per dPKMS instance
// (per ADR-065 §Decision; multi-platform composes via ADR-064
// federation, not multi-backend slots).
const Protocol = "email"
