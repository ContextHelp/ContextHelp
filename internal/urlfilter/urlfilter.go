// Package urlfilter decides whether a browser URL may be captured.
//
// Every browser capture path (open tabs, history sweeps, the ambient
// browserhistory source) runs URLs through one Filter before anything is
// sent for ingestion. Deny is a privacy control, so the filter fails
// closed: an unparseable URL is dropped, and a rule that cannot be
// compiled is an error at construction (or, for uncompiled Rules, denies
// everything).
//
// Rule syntax, one string per rule:
//
//	*://crm.example.net/*        URL form: scheme://host[:port][/path]
//	*://*.example.net/*          *.d matches d and every subdomain of d
//	https://drive.example.com/x  path without * is a prefix
//	*://localhost:8080/*         port pinned; no port means any port
//	about:*                      legacy form: glob over the raw URL
//	example.com                  legacy form without *: substring
//
// URL-form rules compare scheme and host case-insensitively, ignore
// userinfo and fragments, drop a trailing root dot and compare IDN hosts
// in punycode. A URL-form rule never matches a URL without "//"
// (about:blank, mailto:...); use the legacy form for those.
package urlfilter

import (
	"fmt"
	"log/slog"
)

// Rule lists.
const (
	ListDeny      = "deny"
	ListAllowOnly = "allow_only"
)

// Reason says why a Decision came out the way it did.
type Reason string

// Decision reasons.
const (
	// ReasonAllowed: no deny rule matched and every allow_only list that
	// applies was satisfied.
	ReasonAllowed Reason = "allowed"
	// ReasonDenyRule: Decision.Rule is the deny rule that matched.
	ReasonDenyRule Reason = "deny_rule"
	// ReasonNotAllowListed: the scope in Decision.Rule has an allow_only
	// list and none of its rules matched.
	ReasonNotAllowListed Reason = "not_allow_listed"
	// ReasonUnparseable: the URL could not be parsed; dropped fail-closed.
	ReasonUnparseable Reason = "unparseable_url"
	// ReasonInvalidRule: Decision.Rule is a deny rule that does not
	// compile; uncompiled Rules treat it as matching everything.
	ReasonInvalidRule Reason = "invalid_rule"
)

// Rule identifies one configured pattern.
type Rule struct {
	Pattern string
	// List is ListDeny or ListAllowOnly.
	List string
	// Scope names where the rule came from: "builtin", "global",
	// "browser:<name>" or "profile:<browser>/<profile>".
	Scope string
}

// Decision is the outcome of evaluating one URL. It never holds the URL,
// so it is safe to log.
type Decision struct {
	Allowed bool
	Reason  Reason
	// Rule is the rule responsible for a drop; zero when allowed or
	// unparseable. For ReasonNotAllowListed only List and Scope are set.
	Rule Rule
}

// String renders the decision for a --dry-run listing.
func (d Decision) String() string {
	switch d.Reason {
	case ReasonAllowed:
		return "allowed"
	case ReasonDenyRule:
		return fmt.Sprintf("denied by %s rule %q (%s)", d.Rule.List, d.Rule.Pattern, d.Rule.Scope)
	case ReasonNotAllowListed:
		return fmt.Sprintf("denied: no allow_only rule matched (%s)", d.Rule.Scope)
	case ReasonInvalidRule:
		return fmt.Sprintf("denied: invalid %s rule %q (%s)", d.Rule.List, d.Rule.Pattern, d.Rule.Scope)
	default:
		return "denied: unparseable URL"
	}
}

// LogValue implements slog.LogValuer: reason and rule, never the URL.
func (d Decision) LogValue() slog.Value {
	attrs := []slog.Attr{slog.String("reason", string(d.Reason))}
	if d.Rule.Scope != "" {
		attrs = append(attrs,
			slog.String("list", d.Rule.List),
			slog.String("scope", d.Rule.Scope))
	}
	if d.Rule.Pattern != "" {
		attrs = append(attrs, slog.String("rule", d.Rule.Pattern))
	}
	return slog.GroupValue(attrs...)
}

// Evaluator is implemented by Rules and *Filter.
type Evaluator interface {
	Evaluate(rawURL string) Decision
}

// Rules is one allow/deny URL-pattern list.
type Rules struct {
	// Deny: if any pattern matches, the URL is dropped.
	Deny []string `mapstructure:"deny" yaml:"deny,omitempty"`
	// AllowOnly: if non-empty, only URLs matching at least one pattern
	// pass. Empty means allow all (subject to Deny).
	AllowOnly []string `mapstructure:"allow_only" yaml:"allow_only,omitempty"`
}

// Matches reports whether the URL passes the rules.
func (r Rules) Matches(rawURL string) bool { return r.Evaluate(rawURL).Allowed }

// Evaluate checks the URL against the rules, compiling them on each call.
// A deny rule that fails to compile denies every URL; an allow_only rule
// that fails to compile matches none. Use New to surface such errors
// up front.
func (r Rules) Evaluate(rawURL string) Decision {
	t, err := parseTarget(rawURL)
	if err != nil {
		return Decision{Reason: ReasonUnparseable}
	}
	l := compileLayerLenient(Layer{Scope: "rules", Rules: r})
	return evaluate(t, []compiledLayer{l})
}

// Layer is a Rules list tagged with the scope it came from.
type Layer struct {
	Scope string
	Rules
}

// Filter is a compiled, layered rule set. A URL passes when it parses,
// no deny rule in any layer matches, and every layer with a non-empty
// allow_only list has a matching allow_only rule. Layers can therefore
// only narrow what gets captured, never widen it.
type Filter struct {
	layers []compiledLayer
}

type compiledLayer struct {
	scope     string
	deny      []pattern
	allowOnly []pattern
	// invalidDeny holds deny rules that failed to compile (lenient mode).
	invalidDeny []string
}

// New compiles layers into a Filter. Any rule that does not compile is
// an error naming the rule and its scope.
func New(layers ...Layer) (*Filter, error) {
	f := &Filter{layers: make([]compiledLayer, 0, len(layers))}
	for _, l := range layers {
		cl := compiledLayer{scope: l.Scope}
		for _, raw := range l.Deny {
			p, err := compilePattern(raw)
			if err != nil {
				return nil, fmt.Errorf("urlfilter: %s deny rule %q: %w", l.Scope, raw, err)
			}
			cl.deny = append(cl.deny, p)
		}
		for _, raw := range l.AllowOnly {
			p, err := compilePattern(raw)
			if err != nil {
				return nil, fmt.Errorf("urlfilter: %s allow_only rule %q: %w", l.Scope, raw, err)
			}
			cl.allowOnly = append(cl.allowOnly, p)
		}
		f.layers = append(f.layers, cl)
	}
	return f, nil
}

// Evaluate checks the URL against every layer. A nil Filter still drops
// unparseable URLs.
func (f *Filter) Evaluate(rawURL string) Decision {
	t, err := parseTarget(rawURL)
	if err != nil {
		return Decision{Reason: ReasonUnparseable}
	}
	if f == nil {
		return Decision{Allowed: true, Reason: ReasonAllowed}
	}
	return evaluate(t, f.layers)
}

// Matches reports whether the URL passes the filter.
func (f *Filter) Matches(rawURL string) bool { return f.Evaluate(rawURL).Allowed }

func compileLayerLenient(l Layer) compiledLayer {
	cl := compiledLayer{scope: l.Scope}
	for _, raw := range l.Deny {
		if p, err := compilePattern(raw); err == nil {
			cl.deny = append(cl.deny, p)
		} else {
			cl.invalidDeny = append(cl.invalidDeny, raw)
		}
	}
	for _, raw := range l.AllowOnly {
		if p, err := compilePattern(raw); err == nil {
			cl.allowOnly = append(cl.allowOnly, p)
		} else {
			// Keep the list non-empty so the allow_only gate still
			// applies; an uncompilable rule just matches nothing.
			cl.allowOnly = append(cl.allowOnly, pattern{raw: raw, never: true})
		}
	}
	return cl
}

func evaluate(t target, layers []compiledLayer) Decision {
	for _, l := range layers {
		if len(l.invalidDeny) > 0 {
			return Decision{Reason: ReasonInvalidRule, Rule: Rule{Pattern: l.invalidDeny[0], List: ListDeny, Scope: l.scope}}
		}
		for _, p := range l.deny {
			if p.match(t) {
				return Decision{Reason: ReasonDenyRule, Rule: Rule{Pattern: p.raw, List: ListDeny, Scope: l.scope}}
			}
		}
	}
	for _, l := range layers {
		if len(l.allowOnly) == 0 {
			continue
		}
		matched := false
		for _, p := range l.allowOnly {
			if p.match(t) {
				matched = true
				break
			}
		}
		if !matched {
			return Decision{Reason: ReasonNotAllowListed, Rule: Rule{List: ListAllowOnly, Scope: l.scope}}
		}
	}
	return Decision{Allowed: true, Reason: ReasonAllowed}
}
