// Package graph provides entity integrity and graph safety validation.
//
// EntityIntegrityGuard — validates entities before write (format, namespace
// ownership, required fields, version linearity).
//
// GraphSafetyValidator — validates edges/mentions before creation (syntax,
// spoofing, orphans, cycles, duplicates).
package graph

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// -------------------------------------------------------------------------
// Errors
// -------------------------------------------------------------------------

// ErrIntegrityViolation wraps a validation failure with provenance context.
type ErrIntegrityViolation struct {
	Rule      string // short rule label
	Entity    string // slug or mention
	Detail    string
	Step      string // pipeline step or actor, if known
	Timestamp time.Time
}

func (e *ErrIntegrityViolation) Error() string {
	if e.Step != "" {
		return fmt.Sprintf("integrity violation [%s] entity=%q step=%q: %s", e.Rule, e.Entity, e.Step, e.Detail)
	}
	return fmt.Sprintf("integrity violation [%s] entity=%q: %s", e.Rule, e.Entity, e.Detail)
}

// Violations is a collection of ErrIntegrityViolation; also implements error.
type Violations []*ErrIntegrityViolation

func (v Violations) Error() string {
	msgs := make([]string, len(v))
	for i, e := range v {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "; ")
}

func (v Violations) AsError() error {
	if len(v) == 0 {
		return nil
	}
	return v
}

// -------------------------------------------------------------------------
// Format patterns
// -------------------------------------------------------------------------

// slugRE matches a valid @namespace.slug mention or a bare namespace.slug.
// namespace: one or more lowercase alphanum/dash segments separated by dots.
// slug:      one or more lowercase alphanum/dash segments separated by dots.
var slugPartRE = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]*$`)

// mentionRE validates the full canonical form "namespace.slug" (≥2 dot-parts).
// The first part is the namespace; the remainder form the slug.
var mentionRE = regexp.MustCompile(`^@?([a-z0-9][a-z0-9\-]*)(\.[a-z0-9][a-z0-9\-]*)+$`)

// -------------------------------------------------------------------------
// EntityIntegrityGuard
// -------------------------------------------------------------------------

// EntityIntegrityGuard validates entities before they are written to storage.
// When knownNamespaces is non-nil, it acts as an allow-list; writes from
// namespaces not in the registry are rejected.
type EntityIntegrityGuard struct {
	// knownNamespaces maps namespace → owning registry URL.
	// nil means "no registry loaded; namespace check disabled".
	knownNamespaces map[string]string
}

// NewEntityIntegrityGuard returns a guard with no namespace registry.
// Call LoadNamespaces to populate it.
func NewEntityIntegrityGuard() *EntityIntegrityGuard {
	return &EntityIntegrityGuard{}
}

// LoadNamespaces registers the canonical namespace→registry mapping.
// Calling this enables namespace-ownership checks on every write.
func (g *EntityIntegrityGuard) LoadNamespaces(ns map[string]string) {
	g.knownNamespaces = ns
}

// Validate runs all integrity rules against entity. Returns Violations (never nil
// on failure) or nil on success. ctx is reserved for future async checks.
func (g *EntityIntegrityGuard) Validate(_ context.Context, entity *storage.Entity, step string) Violations {
	var v Violations

	v.add(g.checkRequiredFields(entity, step))
	v.add(g.checkSlugFormat(entity, step))
	v.add(g.checkNamespaceOwnership(entity, step))

	return v
}

// ValidateAndGuard is a convenience that returns the first error, suitable for
// use in hot write paths where you just want a go/no-go.
func (g *EntityIntegrityGuard) ValidateAndGuard(ctx context.Context, entity *storage.Entity, step string) error {
	vs := g.Validate(ctx, entity, step)
	if len(vs) == 0 {
		return nil
	}
	return vs[0]
}

// checkRequiredFields ensures id-like fields are present.
func (g *EntityIntegrityGuard) checkRequiredFields(e *storage.Entity, step string) *ErrIntegrityViolation {
	var missing []string
	if e.Slug == "" {
		missing = append(missing, "slug")
	}
	if e.Title == "" {
		missing = append(missing, "title")
	}
	if e.Namespace == "" {
		missing = append(missing, "namespace")
	}
	if len(missing) == 0 {
		return nil
	}
	return &ErrIntegrityViolation{
		Rule:      "required_fields",
		Entity:    e.Slug,
		Detail:    "missing required fields: " + strings.Join(missing, ", "),
		Step:      step,
		Timestamp: time.Now(),
	}
}

// checkSlugFormat validates canonical lowercase slug formatting.
func (g *EntityIntegrityGuard) checkSlugFormat(e *storage.Entity, step string) *ErrIntegrityViolation {
	// slug must be lowercase
	if e.Slug != strings.ToLower(e.Slug) {
		return &ErrIntegrityViolation{
			Rule:      "slug_format",
			Entity:    e.Slug,
			Detail:    "slug must be lowercase",
			Step:      step,
			Timestamp: time.Now(),
		}
	}

	// namespace must be lowercase
	if e.Namespace != strings.ToLower(e.Namespace) {
		return &ErrIntegrityViolation{
			Rule:      "namespace_format",
			Entity:    e.Slug,
			Detail:    "namespace must be lowercase",
			Step:      step,
			Timestamp: time.Now(),
		}
	}

	// each part of namespace and slug must match slugPartRE
	allParts := strings.FieldsFunc(e.Slug, func(r rune) bool { return r == '.' || r == '/' })
	for _, part := range allParts {
		if part == "" || !slugPartRE.MatchString(part) {
			return &ErrIntegrityViolation{
				Rule:      "slug_format",
				Entity:    e.Slug,
				Detail:    fmt.Sprintf("invalid slug segment %q (must be lowercase alphanum/dash)", part),
				Step:      step,
				Timestamp: time.Now(),
			}
		}
	}

	nsParts := strings.Split(e.Namespace, ".")
	for _, part := range nsParts {
		if part == "" || !slugPartRE.MatchString(part) {
			return &ErrIntegrityViolation{
				Rule:      "namespace_format",
				Entity:    e.Slug,
				Detail:    fmt.Sprintf("invalid namespace segment %q (must be lowercase alphanum/dash)", part),
				Step:      step,
				Timestamp: time.Now(),
			}
		}
	}

	return nil
}

// checkNamespaceOwnership rejects entities whose namespace is unknown when a
// registry has been loaded.
func (g *EntityIntegrityGuard) checkNamespaceOwnership(e *storage.Entity, step string) *ErrIntegrityViolation {
	if g.knownNamespaces == nil {
		return nil
	}
	if _, ok := g.knownNamespaces[e.Namespace]; ok {
		return nil
	}
	return &ErrIntegrityViolation{
		Rule:      "namespace_ownership",
		Entity:    e.Slug,
		Detail:    fmt.Sprintf("namespace %q not in registry; possible spoofing or unknown source", e.Namespace),
		Step:      step,
		Timestamp: time.Now(),
	}
}

// -------------------------------------------------------------------------
// GraphSafetyValidator
// -------------------------------------------------------------------------

// GraphSafetyValidator validates graph edges and mentions before creation.
type GraphSafetyValidator struct {
	entities storage.EntityStore
}

// NewGraphSafetyValidator returns a validator backed by the given entity store.
func NewGraphSafetyValidator(es storage.EntityStore) *GraphSafetyValidator {
	return &GraphSafetyValidator{entities: es}
}

// ValidateMention checks that s is a well-formed mention string (@namespace.slug
// or ctxt://entity/…) and that it references an entity that exists in the store.
func (v *GraphSafetyValidator) ValidateMention(ctx context.Context, mention, step string) error {
	if err := validateMentionSyntax(mention); err != nil {
		return &ErrIntegrityViolation{
			Rule:      "mention_syntax",
			Entity:    mention,
			Detail:    err.Error(),
			Step:      step,
			Timestamp: time.Now(),
		}
	}

	// Resolve to slug for store lookup.
	slug := mentionToSlug(mention)
	if _, err := v.entities.Resolve(ctx, slug); err != nil {
		return &ErrIntegrityViolation{
			Rule:      "mention_unresolved",
			Entity:    mention,
			Detail:    fmt.Sprintf("entity %q not found in store", slug),
			Step:      step,
			Timestamp: time.Now(),
		}
	}
	return nil
}

// ValidateEdge checks that an edge is safe to create: both endpoints must be
// well-formed, the target entity must exist, and the namespace is not spoofed.
func (v *GraphSafetyValidator) ValidateEdge(ctx context.Context, edge *storage.Edge, step string) Violations {
	var vs Violations

	if edge.ToType == "entity" {
		// Validate entity target exists and slug is canonical.
		if err := validateMentionSyntax(edge.ToID); err != nil {
			vs = append(vs, &ErrIntegrityViolation{
				Rule:      "edge_target_syntax",
				Entity:    edge.ToID,
				Detail:    err.Error(),
				Step:      step,
				Timestamp: time.Now(),
			})
		} else {
			slug := mentionToSlug(edge.ToID)
			if _, err := v.entities.Resolve(ctx, slug); err != nil {
				vs = append(vs, &ErrIntegrityViolation{
					Rule:      "edge_target_missing",
					Entity:    edge.ToID,
					Detail:    "target entity not found; backlink to nonexistent entity rejected",
					Step:      step,
					Timestamp: time.Now(),
				})
			}
		}
	}

	return vs
}

// DetectCycles performs a depth-first search on the entity backlink graph
// starting from rootEntityID. Returns an error if a cycle is detected.
// edges is a function that returns outgoing edge targets for a given node ID.
func DetectCycles(ctx context.Context, rootID string, outEdges func(ctx context.Context, id string) ([]string, error)) error {
	visited := map[string]bool{}
	inStack := map[string]bool{}
	return dfs(ctx, rootID, visited, inStack, outEdges, nil)
}

func dfs(ctx context.Context, id string, visited, inStack map[string]bool,
	outEdges func(ctx context.Context, id string) ([]string, error),
	path []string,
) error {
	if inStack[id] {
		cycle := append(path, id) //nolint:gocritic // intentional append
		return fmt.Errorf("cycle detected in entity backlink graph: %s", strings.Join(cycle, " → "))
	}
	if visited[id] {
		return nil
	}
	visited[id] = true
	inStack[id] = true
	path = append(path, id)

	targets, err := outEdges(ctx, id)
	if err != nil {
		return fmt.Errorf("dfs outEdges(%q): %w", id, err)
	}
	for _, t := range targets {
		if err := dfs(ctx, t, visited, inStack, outEdges, path); err != nil {
			return err
		}
	}

	inStack[id] = false
	return nil
}

// FindOrphanedEdges returns edge IDs whose target entity does not exist.
// It queries the store's EdgeStore for edges of toType=="entity" and checks
// each target against EntityStore.
func FindOrphanedEdges(ctx context.Context, es storage.EntityStore, edgeStore storage.EdgeStore, fromType, fromID string) ([]string, error) {
	edges, err := edgeStore.ListFrom(ctx, fromType, fromID)
	if err != nil {
		return nil, fmt.Errorf("list edges: %w", err)
	}
	var orphans []string
	for _, e := range edges {
		if e.ToType != "entity" {
			continue
		}
		slug := mentionToSlug(e.ToID)
		if _, err := es.Resolve(ctx, slug); err != nil {
			orphans = append(orphans, e.ID)
		}
	}
	return orphans, nil
}

// FindDuplicateMentions returns slugs that appear more than once in the list.
func FindDuplicateMentions(mentions []string) []string {
	counts := map[string]int{}
	for _, m := range mentions {
		slug := mentionToSlug(m)
		counts[slug]++
	}
	var dups []string
	for slug, n := range counts {
		if n > 1 {
			dups = append(dups, slug)
		}
	}
	return dups
}

// -------------------------------------------------------------------------
// Helpers
// -------------------------------------------------------------------------

// validateMentionSyntax checks that s is a valid @namespace.slug or ctxt:// URI.
func validateMentionSyntax(s string) error {
	if strings.HasPrefix(s, "ctxt://") {
		// minimal URI check: must have at least ctxt://entity/<namespace>/<slug>
		rest := strings.TrimPrefix(s, "ctxt://entity/")
		if rest == s || rest == "" { // didn't trim, or empty after
			return fmt.Errorf("malformed ctxt:// URI %q", s)
		}
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("ctxt:// URI must have namespace and slug segments: %q", s)
		}
		for _, p := range strings.Split(rest, "/") {
			if !slugPartRE.MatchString(p) {
				return fmt.Errorf("ctxt:// URI segment %q is not canonical lowercase alphanum/dash: %q", p, s)
			}
		}
		return nil
	}
	if !mentionRE.MatchString(s) {
		return fmt.Errorf("invalid mention format %q (want @namespace.slug or ctxt://entity/…)", s)
	}
	return nil
}

// mentionToSlug converts @namespace.slug or ctxt://entity/… to the storage slug
// used by EntityStore.Resolve.
func mentionToSlug(s string) string {
	s = strings.TrimPrefix(s, "@")
	if strings.HasPrefix(s, "ctxt://entity/") {
		// ctxt://entity/stripe/api/checkout → stripe.api.checkout
		rest := strings.TrimPrefix(s, "ctxt://entity/")
		return strings.ReplaceAll(rest, "/", ".")
	}
	// @stripe.api.checkout or stripe.api.checkout → stripe.api.checkout
	return s
}

// add appends a non-nil violation to the receiver.
func (v *Violations) add(e *ErrIntegrityViolation) {
	if e != nil {
		*v = append(*v, e)
	}
}

// ErrCycle is returned by DetectCycles when a cycle exists.
var ErrCycle = errors.New("cycle detected in entity backlink graph")
