package cliformat

import (
	"io"

	"hop.top/kit/go/console/output"
)

// This file carries the preview contract shared by every ctxt and
// dpkms command that can be asked what it would do before it does it.
//
// A preview is not narration. An operator gating a reclaim pass on the
// only copy of their knowledge is increasingly a program, and a program
// cannot approve a progress log: it needs to read, field by field,
// whether the command ran, which targets it would touch and how many
// matched. "Would remove 0" and "would remove 40000" warrant very
// different next steps, and prose makes the two indistinguishable to
// anything but a human eye.
//
// The shapes below are kit's cli.Plan/cli.Effect re-expressed with the
// field names this contract names on the wire. kit's own struct tags
// spell the same concepts `command`/`effects`, so adopting it verbatim
// would answer a request for a plan with keys no caller was told to
// read. The Go field NAMES are kept identical to kit's (Command,
// GeneratedAt, Effects) because output.RenderPlan reflects on those
// names to build its table view — so the table rendering comes from
// kit unchanged while the machine document says what it must.

// Action is one entry in a preview plan: a single state change the
// command would apply if re-issued without the preview flag.
//
// Kind/Target/Reversible/Detail mirror kit's cli.Effect field for
// field. Count is the addition: a maintenance step's unit of work is
// usually a row population rather than one named resource, and a
// reviewer approving a prune needs the magnitude, not just the verb.
//
// Field order is chosen for struct alignment (strings, then the
// int64, then the bool) rather than for reading order; the JSON tags
// fix the wire order that callers actually see.
type Action struct {
	// Kind is a short verb naming the operation: "delete", "vacuum",
	// "checkpoint", "reindex".
	Kind string `json:"kind" table:"KIND"`
	// Target is the addressable resource the operation acts on:
	// "object:obj_deadbeef", "table:jobs", the database path.
	Target string `json:"target" table:"TARGET"`
	// Detail is a free-form one-line note for the human table view.
	Detail string `json:"detail,omitempty" table:"DETAIL"`
	// Count is how many units the action would affect. Rendered
	// always, including zero: an omitted count and a count of zero
	// are different answers, and only one of them is safe to ignore.
	Count int64 `json:"count" table:"COUNT"`
	// Reversible reports whether an inverse command can restore prior
	// state without data loss.
	Reversible bool `json:"reversible" table:"REVERSIBLE"`
}

// Plan is the reviewable projection a previewable command returns
// instead of doing its work.
//
// DryRun is always true and always present. A reviewer must be able to
// refuse to proceed the moment it discovers it was handed a result
// where it asked for a projection, and inferring that from the absence
// of an error is guesswork.
//
// Field order is chosen for struct alignment; the JSON tags fix the
// wire order that callers actually see.
type Plan struct {
	// Command is the canonical command path that produced the plan.
	Command string `json:"command" table:"COMMAND"`
	// Effects is the ordered list of state changes the command would
	// apply. Never null: a plan with no actions is an empty list, and
	// a caller iterating it must not have to special-case null.
	//
	// The Go name is Effects, not Actions, so that kit's
	// output.RenderPlan finds it: its table renderer looks the field
	// up by that literal name, and a rename here would leave the
	// human view printing a bare EFFECTS header over no rows. The
	// wire name is "actions", which is what the contract specifies.
	Effects []Action `json:"actions"`
	// Warnings carries non-fatal advisories to surface before the
	// operator approves the plan.
	Warnings []string `json:"warnings,omitempty"`
	// Matched is the total number of units across every action — the
	// one figure a reviewer reads before deciding whether to look
	// closer.
	Matched int64 `json:"matched"`
	// DryRun marks this document a projection rather than a result.
	DryRun bool `json:"dry_run"`
}

// NewPlan builds a plan for command with actions, deriving Matched by
// summing the actions' counts so the total can never disagree with the
// rows it summarizes.
//
// A nil actions slice becomes an empty one here rather than at encode
// time: Plan is also handed to output.RenderPlan, which reflects over
// the live value and never sees cliformat.Encode's normalisation.
func NewPlan(command string, actions []Action) Plan {
	if actions == nil {
		actions = []Action{}
	}
	var matched int64
	for _, a := range actions {
		matched += a.Count
	}
	return Plan{
		DryRun:  true,
		Command: command,
		Effects: actions,
		Matched: matched,
	}
}

// WithWarning appends a non-fatal advisory to the plan.
func (p *Plan) WithWarning(msg string) {
	p.Warnings = append(p.Warnings, msg)
}

// Refusal is the reviewable projection of a destructive command whose
// confirmation gate declined.
//
// It answers the one question a preview exists to answer — did this
// run change anything — in a field rather than by implication, and
// names what a committed run would have touched so the caller can
// decide whether to commit at all.
//
// Applied is always false. A caller branching on it never has to
// distinguish "declined, nothing happened" from "skipped the question
// and proceeded", which is exactly the ambiguity an exit code and a
// prose banner leave open.
//
// Field order is chosen for struct alignment; the JSON tags fix the
// wire order that callers actually see.
type Refusal struct {
	// Command is the canonical command path that was declined.
	Command string `json:"command" table:"COMMAND"`
	// Reason states, in one caller-facing line, why nothing was done.
	Reason string `json:"reason,omitempty"`
	// Targets names the resources a committed run would act on.
	// Never null.
	Targets []string `json:"targets"`
	// Matched is how many objects the filter selected. Distinct from
	// len(Targets), which a listing cap can truncate.
	Matched int64 `json:"matched"`
	// Applied reports whether the operation was carried out. Always
	// false on this document.
	Applied bool `json:"applied"`
}

// NewRefusal builds a refusal document for command over the given
// targets. matched is passed separately rather than derived from
// len(targets): the listing that produced the targets is capped, and
// reporting the capped length as the match count would understate a
// delete an operator is about to approve.
func NewRefusal(command string, targets []string, matched int64, reason string) Refusal {
	if targets == nil {
		targets = []string{}
	}
	return Refusal{
		Applied: false,
		Command: command,
		Targets: targets,
		Matched: matched,
		Reason:  reason,
	}
}

// EncodePreview writes a preview document to w in the active format.
//
// Machine formats get the document itself through Encode. Everything
// else goes through kit's output.RenderPlan, whose table renderer
// reflects on the Command/GeneratedAt/Effects field names — so the
// human view is kit's, unchanged, and only the machine document
// carries this contract's key names.
//
// RenderPlan rejects a format it does not know, and it knows only
// json, yaml and table. csv and text reach it as table: these are
// single documents, not row collections, and a plan flattened to one
// CSV line loses the per-action rows that make it reviewable.
func EncodePreview(w io.Writer, v any) error {
	if Structured() {
		return Encode(w, v)
	}
	return output.RenderPlan(w, output.Table, v)
}
