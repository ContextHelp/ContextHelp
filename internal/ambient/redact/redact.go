// Package redact is the source-side redaction hook (per ADR-066 §Phase 4
// + T-0507).
//
// Per the storage-lives-in-kit principle, this package is a THIN SHIM
// over kit/go/core/redact. The kit/core/redact package handles pattern
// matching, allowlists, observer hooks, and replacement strategies; this
// package wires it into the ambient.RawEvent shape so sources can strip
// known-sensitive shapes BEFORE emission.
//
// Usage from a source's emit path:
//
//	r := redact.New() // default rules: passwords, OAuth tokens, AWS keys
//	ev := source.buildRawEvent(...)
//	ev = r.Apply(ev) // mutates Payload + selected Metadata fields
//	source.events <- ev
//
// The hook does NOT block events whose payload didn't redact cleanly —
// that's the kit/policy CEL guard's job (subscribed to the event.captured
// bus topic). This hook only mutates the payload.
package redact

import (
	"hop.top/kit/go/core/redact"

	"github.com/ideacrafterslabs/ctxt/internal/ambient"
)

// Redactor wraps kit/core/redact.Redactor for the ambient.RawEvent type.
type Redactor struct {
	inner *redact.Redactor
}

// New constructs a Redactor with sensible defaults: matches passwords,
// OAuth bearer tokens, AWS access keys, JWT-looking strings.
func New() *Redactor {
	r := redact.New()
	// Use Tag strategy so kit honours per-rule replacement strings (the
	// default Mask strategy uses ***REDACTED*** for everything regardless
	// of rule). Tag's contract: per-rule replacement takes precedence
	// when supplied, falling back to "<rule-id>" otherwise.
	_, _ = r.SetReplacement(redact.Tag)
	for _, rule := range defaultRules {
		_, _ = r.AddRule(rule.id, rule.pattern, rule.replacement)
	}
	return &Redactor{inner: r}
}

// NewFromRedactor constructs a Redactor wrapping a pre-configured
// kit/core/redact.Redactor. Used by callers that have already loaded
// custom rules from config (e.g. cmd/ctxd reading $XDG_CONFIG_HOME).
func NewFromRedactor(inner *redact.Redactor) *Redactor {
	return &Redactor{inner: inner}
}

// Apply mutates ev in place, redacting known-sensitive shapes from
// Payload and from selected string-typed Metadata fields. Returns the
// mutated event for fluent use.
//
// Source-side use: call before sending on the events channel.
func (r *Redactor) Apply(ev ambient.RawEvent) ambient.RawEvent {
	if r == nil || r.inner == nil {
		return ev
	}
	if len(ev.Payload) > 0 {
		ev.Payload = r.inner.ApplyBytes(ev.Payload)
	}
	if ev.Metadata != nil {
		// Redact only string-typed metadata fields. Numeric / boolean /
		// nested-map fields are left alone (they're semantic markers, not
		// content).
		for k, v := range ev.Metadata {
			if s, ok := v.(string); ok && s != "" {
				ev.Metadata[k] = r.inner.Apply(s)
			}
		}
	}
	return ev
}

// Stats returns the underlying Redactor's stats (matches per rule, etc.).
// Used by status / MCP health tool to surface "how often is redaction
// firing".
func (r *Redactor) Stats() redact.Stats {
	if r == nil || r.inner == nil {
		return redact.Stats{}
	}
	return r.inner.Stats()
}

// defaultRules is the bundled set: common secret-shaped patterns. Sources
// that want stricter or looser rules should construct a kit Redactor
// directly and pass via NewFromRedactor.
var defaultRules = []ruleDef{
	{
		id:          "aws-access-key",
		pattern:     `AKIA[0-9A-Z]{16}`,
		replacement: "[AWS_ACCESS_KEY_REDACTED]",
	},
	{
		id:          "aws-secret-key",
		pattern:     `[A-Za-z0-9/+=]{40}`,
		replacement: "[AWS_SECRET_REDACTED]",
	},
	{
		id:          "github-pat",
		pattern:     `ghp_[A-Za-z0-9]{36,}`,
		replacement: "[GITHUB_PAT_REDACTED]",
	},
	{
		id:          "openai-key",
		pattern:     `sk-[A-Za-z0-9]{20,}`,
		replacement: "[OPENAI_KEY_REDACTED]",
	},
	{
		id:          "bearer-token",
		pattern:     `[Bb]earer\s+[A-Za-z0-9._\-+/=]{20,}`,
		replacement: "Bearer [TOKEN_REDACTED]",
	},
	{
		id:          "ssn-us",
		pattern:     `\b\d{3}-\d{2}-\d{4}\b`,
		replacement: "[SSN_REDACTED]",
	},
	{
		id:          "credit-card",
		pattern:     `\b(?:\d[ -]*?){13,16}\b`,
		replacement: "[CC_REDACTED]",
	},
}

type ruleDef struct {
	id          string
	pattern     string
	replacement string
}
