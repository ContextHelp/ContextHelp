// Package cliconv carries thin, declarative ctxt-side wrappers around the
// hop.top/kit "kit/cli" annotation helpers. The goal is one-liner call
// sites in the cobra command tree, so the 12fcc conformance pass reads
// cleanly across the ~160 leaves.
//
// All wrappers delegate to kit (cliconv has zero opinion about the key
// strings); see kit/go/console/cli/{contract,sideeffect,idempotent,shape}.go
// for the source of truth. Adopters who need a key kit does not yet
// expose can still set cmd.Annotations directly.
package cliconv

import (
	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
)

// SideEffect classifies state mutation. Re-exported aliases so call
// sites in cmd/ctxt/cmd do not need a second kit/cli import.
type SideEffect = kitcli.SideEffect

const (
	SideEffectRead              = kitcli.SideEffectRead
	SideEffectWrite             = kitcli.SideEffectWrite
	SideEffectWriteLocal        = kitcli.SideEffectWriteLocal
	SideEffectWriteShared       = kitcli.SideEffectWriteShared
	SideEffectDestructive       = kitcli.SideEffectDestructive
	SideEffectDestructiveLocal  = kitcli.SideEffectDestructiveLocal
	SideEffectDestructiveShared = kitcli.SideEffectDestructiveShared
	SideEffectInteractive       = kitcli.SideEffectInteractive
)

// Idempotency classifies replay safety.
type Idempotency = kitcli.Idempotency

const (
	IdempotencyYes         = kitcli.IdempotencyYes
	IdempotencyNo          = kitcli.IdempotencyNo
	IdempotencyConditional = kitcli.IdempotencyConditional
)

// WithSideEffect stamps kit/side-effect on cmd. Call once per runnable
// leaf. Pass one of the SideEffect* constants; arbitrary strings are
// rejected by kit's Root.Validate.
func WithSideEffect(cmd *cobra.Command, s SideEffect) {
	kitcli.SetSideEffect(cmd, s)
}

// WithIdempotency stamps kit/idempotent on cmd. Kit auto-applies a
// verb-default for list/show/get/find/create/add/etc. — only call
// WithIdempotency when the verb is absent from kit's default table or
// when you need to override.
func WithIdempotency(cmd *cobra.Command, i Idempotency) {
	kitcli.SetIdempotency(cmd, i)
}

// MarkTopLevelVerb stamps kit/top-level-verb=true on cmd. Every depth-1
// runnable leaf needs this, otherwise the shape validator rejects it
// (kit treats nounless verbs like `ctxt analyze` as suspicious).
func MarkTopLevelVerb(cmd *cobra.Command) {
	kitcli.SetTopLevelVerb(cmd)
}

// MarkHierarchical stamps kit/hierarchical=true on cmd. Required on
// every intermediate non-runnable node whose leaves sit at depth >= 3
// (e.g. `ctxt profile schema add-rule`, `ctxt lateral eval metrics`).
// The signature validator requires the annotation on every intermediate
// regardless of ancestor reservation status.
func MarkHierarchical(cmd *cobra.Command) {
	kitcli.SetHierarchical(cmd)
}

// MarkPassthrough stamps kit/passthrough=true on cmd. Use it when a
// leaf calls cobra.ArbitraryArgs and forwards `-- args` to a child
// process; kit's signature validator otherwise reports the wide-open
// arg surface as a violation.
func MarkPassthrough(cmd *cobra.Command) {
	kitcli.SetPassthrough(cmd)
}

// MarkExemptValidation stamps kit/exempt-validation=true. Reserved
// for kit-internal compat shims; adopter use is discouraged. Wrapped
// here so ctxt code never hand-rolls cmd.Annotations[...] = "true".
func MarkExemptValidation(cmd *cobra.Command) {
	kitcli.SetExemptValidation(cmd)
}

// WithDryRunRationale stamps kit/dry-run-rationale on a write|destructive
// leaf that intentionally opts out of --dry-run. Reason must be
// 1-200 chars; longer values are rejected by kit. Returns the kit
// error so the caller (typically init()) can panic during boot.
func WithDryRunRationale(cmd *cobra.Command, reason string) error {
	return kitcli.SetDryRunRationale(cmd, reason)
}

// WithDestructiveToken stamps kit/destructive-token=required on a
// destructive leaf, opting it into the typed-token confirmation flow.
func WithDestructiveToken(cmd *cobra.Command) {
	kitcli.SetDestructiveToken(cmd)
}

// WithRetryable stamps kit/retryable on cmd. True marks the effect as
// safely re-runnable after a transient failure.
func WithRetryable(cmd *cobra.Command, v bool) {
	kitcli.SetRetryable(cmd, v)
}
