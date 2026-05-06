// Package calendar is the `calendar` protocol slot under
// internal/adapter/.
//
// Backends (caldav-vdir today; future iCloud / Google Calendar /
// Microsoft 365 / Fastmail) live as subpackages and identify
// themselves to the typed substrate via the Protocol constant defined
// here.
package calendar

// Protocol is the slot identity. Every Adapter under this slot returns
// this string from Adapter.Protocol(). The substrate's Registry uses
// this value to enforce one-platform-per-protocol per dPKMS instance
// (per ADR-065 §Decision; multi-platform composes via ADR-064
// federation, not multi-backend slots).
const Protocol = "calendar"
