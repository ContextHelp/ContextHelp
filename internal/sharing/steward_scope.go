package sharing

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// StewardSettings are the instance settings a steward grant is read
// under (ADR-083 §2). They are read on every request, never cached
// into a viewer: lowering the cap, turning allow_withheld off or adding
// a withheld class takes effect on the next request of a live grant.
type StewardSettings struct {
	// Enabled is steward.enabled: off, no grant is used for reading.
	Enabled bool
	// MaxDuration is steward.max_duration, the hard cap on a grant's
	// window; <= 0 means no cap.
	MaxDuration time.Duration
	// WithheldClasses are removed from every steward scope at compile
	// time (steward.withheld_classes).
	WithheldClasses []string
	// AllowWithheld is steward.allow_withheld: a grant may name a
	// withheld class in include_classes and so receive it.
	AllowWithheld bool
	// AllowExport is steward.allow_export: a scope may open export.
	AllowExport bool
}

// StewardSpec is one policy spec of a steward scope: what a share
// policy selects, renders, masks and opens, without a name.
type StewardSpec struct {
	// Data is the RSQL filter an object must match (Policy.Data).
	Data string
	// ExcludeClasses removes objects and parts carrying any of these
	// classes from what Data matched.
	ExcludeClasses []string
	// Fields and Capabilities are the policy's.
	Fields       map[string][]string
	Capabilities Capabilities
	// Redact is only ever an as_audience spec's: the policy's redact,
	// as the audience's members have it. It is inert until masking
	// lands, then masks for the steward exactly as for the members. An
	// explicit scope never carries one (ParseStewardRequestScope).
	Redact Redact
}

// StewardScope is the scope a steward grant freezes (ADR-083 §2): a
// list of policy specs plus include_classes, with which a grant asks
// for a withheld class. Each spec compiles to a grant of its own, all
// of them the one steward grant's, so an object renders through the
// specs that cover it alone, as it does through an ordinary viewer's
// grants. An explicit scope is a list of one; as_audience freezes an
// audience's policies one spec each (FreezeAudienceScope).
//
// A scope is decoded strictly: a misspelled key is refused, not
// ignored, since a limit left unset in an approved, frozen grant cannot
// be taken back.
type StewardScope struct {
	Specs []StewardSpec
	// IncludeClasses names withheld classes the grant asks for. It has
	// effect only where steward.allow_withheld is on.
	IncludeClasses []string
}

// Redacts reports whether a spec of the scope carries a redact: the
// masking it asks for is not applied yet, which the approvers are told.
func (s StewardScope) Redacts() bool {
	for _, sp := range s.Specs {
		if !redactEmpty(sp.Redact) {
			return true
		}
	}
	return false
}

// stewardSpecWire is the JSON form of a spec. An empty redact and empty
// capabilities are left out, so a frozen spec shows what it asks for.
type stewardSpecWire struct {
	Data           string              `json:"data"`
	ExcludeClasses []string            `json:"exclude_classes,omitempty"`
	Fields         map[string][]string `json:"fields,omitempty"`
	Redact         *Redact             `json:"redact,omitempty"`
	Capabilities   *Capabilities       `json:"capabilities,omitempty"`
}

// stewardScopeWire is the stored form of a scope: the frozen payload's
// and the grant row's.
type stewardScopeWire struct {
	Policies       []stewardSpecWire `json:"policies"`
	IncludeClasses []string          `json:"include_classes,omitempty"`
}

// stewardRequestWire is the form an explicit scope is filed in: one
// spec, with include_classes beside its keys. Its Redact shadows the
// spec's, so a redact of any value, null included, is decoded only to
// be refused with its reason.
type stewardRequestWire struct {
	stewardSpecWire
	Redact         json.RawMessage `json:"redact,omitempty"`
	IncludeClasses []string        `json:"include_classes,omitempty"`
}

// stewardPlaceholderName names the policy a spec is validated as.
const stewardPlaceholderName = "steward-scope"

// ParseStewardScope decodes a frozen scope in its stored form
// ({"policies": [spec, ...], "include_classes": [...]}) strictly and
// validates it: at least one spec; each spec a valid policy (the data
// filter parses and compiles for both dialects over the fields a
// policy may use; the class, field, redact and capability shapes are a
// policy's); no class both included and excluded. A spec's redact is
// the one as_audience froze.
func ParseStewardScope(raw json.RawMessage) (StewardScope, error) {
	var w stewardScopeWire
	if err := decodeStewardScope(raw, &w); err != nil {
		return StewardScope{}, err
	}
	s := StewardScope{IncludeClasses: w.IncludeClasses}
	for _, sw := range w.Policies {
		s.Specs = append(s.Specs, sw.spec())
	}
	if err := s.validate(); err != nil {
		return StewardScope{}, err
	}
	return s, nil
}

// ParseStewardRequestScope decodes an explicit scope as a request files
// it, one spec with include_classes beside its keys ({"data": ...,
// "include_classes": [...]}), strictly, and validates it as
// ParseStewardScope does. A redact is refused: masking is not applied,
// and an approver must not read it as applied. The scope it returns is
// a list of one.
func ParseStewardRequestScope(raw json.RawMessage) (StewardScope, error) {
	var w stewardRequestWire
	if err := decodeStewardScope(raw, &w); err != nil {
		return StewardScope{}, err
	}
	if len(w.Redact) > 0 {
		return StewardScope{}, fmt.Errorf("steward scope: %w", errStewardRedact)
	}
	s := StewardScope{Specs: []StewardSpec{w.spec()}, IncludeClasses: w.IncludeClasses}
	if err := s.validate(); err != nil {
		return StewardScope{}, err
	}
	return s, nil
}

// decodeStewardScope decodes raw into v strictly: one JSON object, no
// unknown key, nothing after it.
func decodeStewardScope(raw json.RawMessage, v any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return errors.New("steward scope: empty")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("steward scope: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("steward scope: trailing data after the object")
	}
	return nil
}

// errStewardRedact refuses a redact in an explicit steward scope.
var errStewardRedact = errors.New("redact: masking is not available in an explicit steward scope; select with data, exclude_classes and fields instead")

// spec is w as a spec.
func (w stewardSpecWire) spec() StewardSpec {
	s := StewardSpec{Data: w.Data, ExcludeClasses: w.ExcludeClasses, Fields: w.Fields}
	if w.Redact != nil {
		s.Redact = *w.Redact
	}
	if w.Capabilities != nil {
		s.Capabilities = *w.Capabilities
	}
	return s
}

// validate checks every spec as a policy, and the include_classes.
func (s StewardScope) validate() error {
	var errs []error
	if len(s.Specs) == 0 {
		errs = append(errs, errors.New("a scope needs at least one policy spec"))
	}
	for i, sp := range s.Specs {
		if err := sp.policy(stewardPlaceholderName, 1).Validate(); err != nil {
			errs = append(errs, s.at(i, err))
		}
	}
	for _, c := range s.IncludeClasses {
		if err := storage.ValidateClassName(c); err != nil {
			errs = append(errs, fmt.Errorf("include_classes: %w", err))
		}
		for i, sp := range s.Specs {
			if slices.Contains(sp.ExcludeClasses, c) {
				errs = append(errs, s.at(i, fmt.Errorf("class %q is both included and excluded", c)))
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("steward scope: %w", err)
	}
	return nil
}

// at names spec i in err when the scope has more than one.
func (s StewardScope) at(i int, err error) error {
	if len(s.Specs) < 2 {
		return err
	}
	return fmt.Errorf("policies[%d]: %w", i, err)
}

// policy is the spec as a share policy, as stored: no setting applied.
func (sp StewardSpec) policy(name string, version int) Policy {
	return Policy{
		Name: name, Version: version, Data: sp.Data, ExcludeClasses: sp.ExcludeClasses, Fields: sp.Fields,
		Redact: sp.Redact, Capabilities: sp.Capabilities,
	}
}

// JSON encodes the scope in its stored form.
func (s StewardScope) JSON() (json.RawMessage, error) {
	w := stewardScopeWire{Policies: make([]stewardSpecWire, len(s.Specs)), IncludeClasses: s.IncludeClasses}
	for i, sp := range s.Specs {
		w.Policies[i] = stewardSpecWire{Data: sp.Data, ExcludeClasses: sp.ExcludeClasses, Fields: sp.Fields}
		if !redactEmpty(sp.Redact) {
			r := sp.Redact
			w.Policies[i].Redact = &r
		}
		if !capabilitiesEmpty(sp.Capabilities) {
			c := sp.Capabilities
			w.Policies[i].Capabilities = &c
		}
	}
	b, err := json.Marshal(w)
	if err != nil {
		return nil, fmt.Errorf("steward scope: encode: %w", err)
	}
	return b, nil
}

func capabilitiesEmpty(c Capabilities) bool {
	return len(c.Search) == 0 && !c.Entities && !c.Export && !c.MCP && !c.Events
}

// Check tests the scope against the instance settings when a request
// for it is filed: the feature is on, no spec opens mcp or events, a
// spec opens export only where steward.allow_export is on, and
// include_classes names a withheld class only where
// steward.allow_withheld is on. (The settings are applied again, to
// every grant, when it is used: Policies.)
func (s StewardScope) Check(st StewardSettings) error {
	var errs []error
	if !st.Enabled {
		errs = append(errs, errors.New("steward access is not enabled on this instance (steward.enabled)"))
	}
	for i, sp := range s.Specs {
		if sp.Capabilities.MCP {
			errs = append(errs, s.at(i, errors.New("capabilities.mcp: a steward scope never opens mcp")))
		}
		if sp.Capabilities.Events {
			errs = append(errs, s.at(i, errors.New("capabilities.events: a steward scope never opens events")))
		}
		if sp.Capabilities.Export && !st.AllowExport {
			errs = append(errs, s.at(i, errors.New("capabilities.export: this instance does not allow steward export (steward.allow_export)")))
		}
	}
	if len(s.IncludeClasses) > 0 && !st.AllowWithheld {
		errs = append(errs, errors.New("include_classes: this instance does not allow a steward to receive withheld classes (steward.allow_withheld)"))
	}
	for _, c := range s.IncludeClasses {
		if !slices.Contains(st.WithheldClasses, c) {
			errs = append(errs, fmt.Errorf("include_classes: class %q is not withheld on this instance", c))
		}
	}
	return errors.Join(errs...)
}

// Policies are the policies a steward grant's viewer is compiled from,
// under st: one per spec, in the scope's order, each frozen at version
// 1 and named for the grant and its index ("<grant>/<i>"), its redact
// as frozen, with
//   - the withheld classes added to exclude_classes, except those the
//     grant names in include_classes where steward.allow_withheld is on;
//   - mcp and events forced off, and export off unless
//     steward.allow_export is on.
//
// It is applied per grant at compile time, so none of it touches an
// ordinary view.
func (s StewardScope) Policies(grantID string, st StewardSettings) []Policy {
	out := make([]Policy, len(s.Specs))
	for i, sp := range s.Specs {
		exclude := slices.Clone(sp.ExcludeClasses)
		for _, c := range st.WithheldClasses {
			if st.AllowWithheld && slices.Contains(s.IncludeClasses, c) {
				continue
			}
			exclude = append(exclude, c)
		}
		p := sp.policy(fmt.Sprintf("%s/%d", grantID, i), 1)
		p.ExcludeClasses = storage.NormalizeClasses(exclude)
		p.Capabilities.MCP, p.Capabilities.Events = false, false
		p.Capabilities.Export = p.Capabilities.Export && st.AllowExport
		out[i] = p
	}
	return out
}

// StewardScopeDigest is the digest of a scope's canonical form
// ("v1:sha256:<hex>", the approval payload digest's format): object
// keys sorted, no insignificant whitespace, numbers as written, so the
// same scope has one digest however it was spaced, and any change of
// content has another. It is computed over the stored text, unknown
// keys included, so a row edited behind the application's back does
// not keep its digest.
func StewardScopeDigest(raw json.RawMessage) (string, error) {
	canon, err := canonicalJSON(raw)
	if err != nil {
		return "", fmt.Errorf("steward scope digest: %w", err)
	}
	sum := sha256.Sum256(canon)
	return storage.DigestPrefix + hex.EncodeToString(sum[:]), nil
}

// canonicalJSON returns raw as one JSON value with sorted object keys,
// no insignificant whitespace, no HTML escaping and numbers kept as
// written. It is the canonical form of internal/approval, kept here
// because this package does not depend on it (a parity test in
// internal/steward holds the two together).
func canonicalJSON(raw json.RawMessage) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("canonical json: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("canonical json: trailing data after the value")
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	// encoding/json writes map keys sorted.
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("canonical json: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}
