package sharing

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Policy is a share policy spec (ADR-080 §2): a data filter, the
// classes it excludes, the fields it renders per type, the redaction it
// asks for and the capabilities it opens. The YAML form is what
// `ctxt share policy` reads; the JSON form is what share_policies.spec
// stores. Version is the store's, never the YAML's.
type Policy struct {
	Name    string `yaml:"name" json:"name"`
	Version int    `yaml:"version,omitempty" json:"-"`
	// Data is the RSQL filter (ADR-050) an object must match, compiled to
	// SQL by internal/search. Only PolicyFields may appear in it.
	Data string `yaml:"data" json:"data"`
	// ExcludeClasses removes objects and parts carrying any of these
	// classes from what Data matched.
	ExcludeClasses []string `yaml:"exclude_classes,omitempty" json:"exclude_classes,omitempty"`
	// Fields is the per-type allowlist of rendered fields; the "*" type
	// covers every type without its own entry, and the "*" field, alone
	// in a list, renders every field of the type (FieldAll).
	Fields map[string][]string `yaml:"fields,omitempty" json:"fields,omitempty"`
	// Redact is accepted and stored but inert until span masking lands
	// (plan Phase 4).
	Redact       Redact       `yaml:"redact,omitempty" json:"redact,omitempty"`
	Capabilities Capabilities `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
}

// Redact names the PII categories masked or revealed in visible text,
// and how a masked value is shown.
type Redact struct {
	Mask   []string `yaml:"mask,omitempty" json:"mask,omitempty"`
	Reveal []string `yaml:"reveal,omitempty" json:"reveal,omitempty"`
	// Style is RedactCategory ("[email]") or RedactPartial ("s…@acme.com");
	// empty means RedactCategory.
	Style string `yaml:"style,omitempty" json:"style,omitempty"`
}

// Capabilities are the surfaces a policy opens to its viewers.
type Capabilities struct {
	// Search lists the search modes allowed: SearchModes.
	Search []string `yaml:"search,omitempty" json:"search,omitempty"`
	// Entities opens entities reachable through visible objects.
	Entities bool `yaml:"entities,omitempty" json:"entities,omitempty"`
	Export   bool `yaml:"export,omitempty" json:"export,omitempty"`
	MCP      bool `yaml:"mcp,omitempty" json:"mcp,omitempty"`
	// Events is deprecated and ignored: GET /events is open to every
	// viewer and shows it the object events it may read, whatever this
	// says. It still parses, so existing policies load; setting it warns.
	Events bool `yaml:"events,omitempty" json:"events,omitempty"`
}

// Redaction styles.
const (
	RedactCategory = "category"
	RedactPartial  = "partial"
)

// Search modes a policy may open.
const (
	SearchGraph  = "graph"
	SearchFTS    = "fts"
	SearchVector = "vector"
	SearchRSQL   = "rsql"
)

// SearchModes lists every search mode a policy may name.
var SearchModes = []string{SearchGraph, SearchFTS, SearchVector, SearchRSQL}

// PolicyFields are the RSQL fields a policy's data filter may use: the
// object's own columns, its tags, profile and classes, and the date
// columns. Comparisons against one metadata key (metadata.<key>) are
// allowed too. The cross-object operators (mention, related) and
// full-text matching are refused: a policy decides visibility per row.
var PolicyFields = []string{"type", "subtype", "tag", "profile", "class", "source", "created_at", "updated_at"}

// crossObjectFields reach other rows through the edges table; a policy
// that used them would make an object visible because of a hidden one
// (ADR-080 §2).
var crossObjectFields = map[string]bool{"mention": true, "related": true}

// metadataFieldPrefix is the field prefix of a metadata key comparison.
const metadataFieldPrefix = "metadata."

var (
	fieldNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	typeNamePattern  = regexp.MustCompile(`^(\*|[a-z][a-z0-9_.-]*)$`)
	categoryPattern  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// ParsePolicy decodes one policy from its YAML form and validates it.
// Unknown keys are refused.
func ParsePolicy(data []byte) (Policy, error) {
	policies, err := ParsePolicies(data)
	if err != nil {
		return Policy{}, err
	}
	if len(policies) != 1 {
		return Policy{}, fmt.Errorf("policy: want one document, got %d", len(policies))
	}
	return policies[0], nil
}

// ParsePolicies decodes every policy of a YAML stream (documents
// separated by ---) and validates each. Unknown keys are refused.
func ParsePolicies(data []byte) ([]Policy, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var out []Policy
	for i := 0; ; i++ {
		var p Policy
		err := dec.Decode(&p)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("policy document %d: %w", i+1, err)
		}
		if err := p.Validate(); err != nil {
			return nil, fmt.Errorf("policy document %d: %w", i+1, err)
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, errors.New("policy: no document")
	}
	warnDeclaredEvents(data, out)
	return out, nil
}

// Validate reports every problem with p: the name, the data filter
// (parses, uses PolicyFields or metadata keys only, compiles for both
// dialects), the classes, the field allowlists, the redaction shape and
// the capability names.
func (p Policy) Validate() error {
	var errs []error
	if err := storage.ValidateSharingName("policy", p.Name); err != nil {
		errs = append(errs, err)
	}
	if _, err := p.DataNode(); err != nil {
		errs = append(errs, err)
	}
	for _, c := range p.ExcludeClasses {
		if err := storage.ValidateClassName(c); err != nil {
			errs = append(errs, fmt.Errorf("exclude_classes: %w", err))
		}
	}
	errs = append(errs, p.validateFields()...)
	errs = append(errs, p.Redact.validate()...)
	errs = append(errs, p.Capabilities.validate()...)
	if p.Capabilities.Events {
		warnEventsDeprecated(p.Name)
	}
	return errors.Join(errs...)
}

// eventsWarned records the policies that already got the deprecation
// warning, so each warns once per process.
var eventsWarned sync.Map

// warnEventsDeprecated logs, once per policy name, that
// capabilities.events no longer gates anything.
func warnEventsDeprecated(name string) {
	if _, dup := eventsWarned.LoadOrStore(name, struct{}{}); dup {
		return
	}
	slog.Warn("share policy: capabilities.events is deprecated and ignored: the event stream shows a viewer the object events it may read whatever this says; remove the key",
		"policy", name)
}

// warnDeclaredEvents warns for each policy of the stream whose document
// names capabilities.events. The strict decode cannot tell `events:
// false` from no key, so the documents are read again as written.
func warnDeclaredEvents(data []byte, policies []Policy) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for _, p := range policies {
		var doc yaml.Node
		if dec.Decode(&doc) != nil {
			return
		}
		if declaresEvents(&doc) {
			warnEventsDeprecated(p.Name)
		}
	}
}

// declaresEvents reports whether a policy document names
// capabilities.events at all, true or false: a false value decodes to
// the zero Capabilities and could not be told from an absent key.
func declaresEvents(doc *yaml.Node) bool {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value != "capabilities" || doc.Content[i+1].Kind != yaml.MappingNode {
			continue
		}
		caps := doc.Content[i+1]
		for j := 0; j+1 < len(caps.Content); j += 2 {
			if caps.Content[j].Value == "events" {
				return true
			}
		}
	}
	return false
}

func (p Policy) validateFields() []error {
	var errs []error
	for typ, fields := range p.Fields {
		if !typeNamePattern.MatchString(typ) {
			errs = append(errs, fmt.Errorf("fields: type %q: want * or a lowercase type name", typ))
		}
		if len(fields) == 0 {
			errs = append(errs, fmt.Errorf("fields: type %q: list at least one field", typ))
		}
		for _, f := range fields {
			if f == FieldAll {
				if len(fields) != 1 {
					errs = append(errs, fmt.Errorf("fields: type %q: %q renders every field and must be the only entry", typ, FieldAll))
				}
				continue
			}
			if !fieldNamePattern.MatchString(f) {
				errs = append(errs, fmt.Errorf("fields: type %q: field %q: want a lowercase field name", typ, f))
			}
		}
	}
	return errs
}

func (r Redact) validate() []error {
	var errs []error
	switch r.Style {
	case "", RedactCategory, RedactPartial:
	default:
		errs = append(errs, fmt.Errorf("redact.style %q: want %s or %s", r.Style, RedactCategory, RedactPartial))
	}
	for name, cats := range map[string][]string{"mask": r.Mask, "reveal": r.Reveal} {
		for _, c := range cats {
			if !categoryPattern.MatchString(c) {
				errs = append(errs, fmt.Errorf("redact.%s: category %q: want a lowercase name", name, c))
			}
		}
	}
	return errs
}

func (c Capabilities) validate() []error {
	var errs []error
	for _, m := range c.Search {
		if !slices.Contains(SearchModes, m) {
			errs = append(errs, fmt.Errorf("capabilities.search: unknown mode %q (valid: %s)", m, strings.Join(SearchModes, ", ")))
		}
	}
	return errs
}

// DataNode parses the data filter and checks it against PolicyFields.
func (p Policy) DataNode() (search.Node, error) {
	if strings.TrimSpace(p.Data) == "" {
		return nil, errors.New("data: a policy needs a data filter")
	}
	node, err := search.Parse(p.Data)
	if err != nil {
		return nil, fmt.Errorf("data: %w", err)
	}
	if err := checkPolicyFields(node); err != nil {
		return nil, fmt.Errorf("data: %w", err)
	}
	for _, d := range []search.Dialect{search.DialectSQLite, search.DialectPostgres} {
		if _, _, err := search.CompileFor(d, node); err != nil {
			return nil, fmt.Errorf("data: %w", err)
		}
	}
	return node, nil
}

// checkPolicyFields walks node and refuses any field a policy may not
// filter by.
func checkPolicyFields(node search.Node) error {
	switch n := node.(type) {
	case search.ComparisonNode:
		switch {
		case crossObjectFields[n.Field]:
			return fmt.Errorf("field %q reaches across objects and is refused in a policy", n.Field)
		case slices.Contains(PolicyFields, n.Field), strings.HasPrefix(n.Field, metadataFieldPrefix):
			return nil
		default:
			return fmt.Errorf("field %q is not allowed in a policy (valid: %s, metadata.<key>)", n.Field, strings.Join(PolicyFields, ", "))
		}
	case search.AndNode:
		return checkChildren(n.Children)
	case search.OrNode:
		return checkChildren(n.Children)
	default:
		return fmt.Errorf("unknown node %T", node)
	}
}

func checkChildren(children []search.Node) error {
	for _, c := range children {
		if err := checkPolicyFields(c); err != nil {
			return err
		}
	}
	return nil
}

// Spec encodes p as the JSON object share_policies.spec stores. The
// version is left out: the store owns it.
func (p Policy) Spec() (json.RawMessage, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("policy %s: encode: %w", p.Name, err)
	}
	return b, nil
}

// PolicyFromSpec decodes a stored policy row: its spec strictly, its
// name and version from the row (the row wins over the spec's name).
func PolicyFromSpec(row *storage.SharePolicy) (Policy, error) {
	if row == nil {
		return Policy{}, errors.New("policy: nil row")
	}
	dec := json.NewDecoder(bytes.NewReader(row.Spec))
	dec.DisallowUnknownFields()
	var p Policy
	if err := dec.Decode(&p); err != nil {
		return Policy{}, fmt.Errorf("policy %s: decode spec: %w", row.Name, err)
	}
	p.Name = row.Name
	p.Version = row.Version
	if err := p.Validate(); err != nil {
		return Policy{}, fmt.Errorf("policy %s: %w", row.Name, err)
	}
	return p, nil
}

// FieldsFor returns the fields p renders for objects of type typ: the
// type's own entry, else the "*" entry, else none.
func (p Policy) FieldsFor(typ string) []string {
	if f, ok := p.Fields[typ]; ok {
		return f
	}
	return p.Fields["*"]
}
