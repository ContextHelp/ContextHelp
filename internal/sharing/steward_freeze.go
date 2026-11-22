package sharing

import (
	"errors"
	"slices"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// FreezeAudienceScope freezes the policies currently granted to an
// audience into a scope (the as_audience shorthand of ADR-083 §2: "what
// partners could see"), to be frozen into a request. The freeze is the
// point: later edits to a policy or to the audience's grants never
// reach an approved grant.
//
// Each policy becomes a spec of its own, never a merge with the others,
// so the elevated viewer attributes each object to the specs covering
// it exactly as a member's viewer attributes it to the grants covering
// it: per object, the same rows, the same fields and the same surfaces.
// The policies are ordered by name and a repeat counts once, so equal
// audiences freeze to equal scopes and digests.
//
// A policy's redact is frozen unchanged into its spec. Masking is inert
// today for the members and the steward alike; once it lands it masks
// for the steward exactly as for the members, so the elevated view
// never shows more than a member sees (the scope Redacts, and the
// approvers are told the masking is not applied yet).
//
// What a steward scope never opens is dropped, and the capabilities
// dropped are returned, sorted, for the approvers to see: mcp and
// events, and export unless st allows it. No policy at all is refused:
// an empty audience would freeze a scope that opens nothing.
func FreezeAudienceScope(policies []Policy, st StewardSettings) (StewardScope, []string, error) {
	if len(policies) == 0 {
		return StewardScope{}, nil, errors.New("audience grants no policy: nothing to freeze")
	}
	ps := slices.Clone(policies)
	slices.SortStableFunc(ps, func(a, b Policy) int { return strings.Compare(a.Name, b.Name) })
	ps = slices.CompactFunc(ps, func(a, b Policy) bool { return a.Name == b.Name })

	var s StewardScope
	var dropped []string
	for _, p := range ps {
		sp := StewardSpec{Data: p.Data, ExcludeClasses: storage.NormalizeClasses(p.ExcludeClasses), Fields: p.Fields, Capabilities: p.Capabilities}
		sp.Redact = Redact{Mask: slices.Clone(p.Redact.Mask), Reveal: slices.Clone(p.Redact.Reveal), Style: p.Redact.Style}
		sp.Capabilities.Search = slices.Clone(p.Capabilities.Search)
		if sp.Capabilities.MCP {
			sp.Capabilities.MCP, dropped = false, append(dropped, storage.CapabilityMCP)
		}
		if sp.Capabilities.Events {
			sp.Capabilities.Events, dropped = false, append(dropped, storage.CapabilityEvents)
		}
		if sp.Capabilities.Export && !st.AllowExport {
			sp.Capabilities.Export, dropped = false, append(dropped, storage.CapabilityExport)
		}
		s.Specs = append(s.Specs, sp)
	}
	if err := s.validate(); err != nil {
		return StewardScope{}, nil, err
	}
	slices.Sort(dropped)
	return s, slices.Compact(dropped), nil
}

func redactEmpty(r Redact) bool {
	return len(r.Mask) == 0 && len(r.Reveal) == 0 && r.Style == ""
}
