// Package mxhook hosts the email-protocol backends that connect to
// hosted IMAP/SMTP providers (Gmail today; future iCloud, Microsoft
// 365, Proton, Fastmail). Each provider lives in its own subpackage
// and embeds the shared Config defined here so operators see the same
// knobs across providers.
//
// Backends share:
//
//   - OAuthCredentialsRef — a credential URI; the actual OAuth2 token
//     is resolved at Start time via kit/storage/secret. Adapter structs
//     never hold long-lived secrets.
//   - Folder defaulting / MaxItems — IMAP envelope-list shape.
//   - The "fetch envelopes only, fetch bodies on-demand" Phase 2
//     contract: full-message bodies live downstream in pipelines.
//
// Phase 2 ships gmail. Phase 3 broadens the catalog without churning
// the substrate; new backends only need to satisfy the typed
// adapter.Adapter contract.
package mxhook

// OAuthCredentialsRef is a stable URI that resolves to an OAuth2
// credential at Start time. The adapter never persists the resolved
// token; it lives in the live IMAP/SMTP connection state and is
// re-resolved on reconnect.
//
// Recommended URI schemes (Phase 2):
//
//   - "env:<PREFIX>" — read from environment via kit/storage/secret/env.
//     Example: "env:GMAIL" expects GMAIL_CLIENT_ID, GMAIL_CLIENT_SECRET,
//     GMAIL_REFRESH_TOKEN.
//   - "keyring:<service>" — Phase 3, integrates kit/storage/secret/keyring.
//   - "openbao:<mount>/<path>" — Phase 3, integrates kit/storage/secret/openbao.
//
// Empty Ref selects the package default (env:GMAIL for gmail). Backends
// document their default in their package doc.
type OAuthCredentialsRef string

// Config is the shared mxhook backend configuration. Provider-specific
// extensions embed this struct so operators see consistent fields
// across providers and so substrate code can probe the common bits.
//
// Zero-value is safe — defaults apply per backend (e.g. gmail folder
// defaults to "INBOX", MaxItems defaults to 100).
type Config struct {
	// CredentialsRef is the OAuth2 credential URI; resolved at Start.
	// See OAuthCredentialsRef for supported schemes.
	CredentialsRef OAuthCredentialsRef

	// Folder is the IMAP folder to poll. Empty means provider default
	// (typically "INBOX").
	Folder string

	// MaxItems caps the number of envelopes returned per Fetch call.
	// 0 means provider default.
	MaxItems int

	// PollInterval governs how often the long-running Start-goroutine
	// polls IMAP. 0 means provider default. Phase 2 uses simple
	// polling; IDLE is a Phase 3 optimization.
	PollIntervalSeconds int
}
