package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"hop.top/kit/go/runtime/domain"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// pipelineRepo adapts storage.PipelineStore to
// domain.Repository[storage.Pipeline].
//
// Pipelines are keyed by Name everywhere in the storage layer (Get,
// Update, Delete, Archive, Unarchive), so the domain.Repository ID
// argument flows through as the pipeline name. The repository keeps
// list/filter behavior identical to the legacy path: domain.Query maps
// to storage.PipelineFilter with archive flags off (active pipelines
// only). Callers that need rich filtering still use ListPipelines
// directly.
type pipelineRepo struct {
	store storage.PipelineStore
}

// Compile-time assertion: pipelineRepo implements
// domain.Repository[storage.Pipeline].
var _ domain.Repository[storage.Pipeline] = (*pipelineRepo)(nil)

func newPipelineRepo(store storage.PipelineStore) *pipelineRepo {
	return &pipelineRepo{store: store}
}

func (r *pipelineRepo) Create(ctx context.Context, p *storage.Pipeline) error {
	if p == nil {
		return fmt.Errorf("%w: pipeline is required", domain.ErrValidation)
	}
	if err := r.store.Create(ctx, p); err != nil {
		return mapPipelineRepoErr(err)
	}
	return nil
}

func (r *pipelineRepo) Get(ctx context.Context, id string) (*storage.Pipeline, error) {
	got, err := r.store.Get(ctx, id)
	if err != nil {
		return nil, mapPipelineRepoErr(err)
	}
	if got == nil {
		return nil, domain.ErrNotFound
	}
	return got, nil
}

func (r *pipelineRepo) List(ctx context.Context, q domain.Query) ([]storage.Pipeline, error) {
	filter := storage.PipelineFilter{}
	pipelines, _, err := r.store.List(ctx, filter)
	if err != nil {
		return nil, mapPipelineRepoErr(err)
	}
	out := make([]storage.Pipeline, 0, len(pipelines))
	for _, p := range pipelines {
		if p == nil {
			continue
		}
		out = append(out, *p)
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (r *pipelineRepo) Update(ctx context.Context, p *storage.Pipeline) error {
	if p == nil {
		return fmt.Errorf("%w: pipeline is required", domain.ErrValidation)
	}
	if err := r.store.Update(ctx, p); err != nil {
		return mapPipelineRepoErr(err)
	}
	return nil
}

func (r *pipelineRepo) Delete(ctx context.Context, id string) error {
	if err := r.store.Delete(ctx, id); err != nil {
		return mapPipelineRepoErr(err)
	}
	return nil
}

// mapPipelineRepoErr translates the loose storage error vocabulary
// into the kit/runtime/domain sentinel set so domain.Service users
// can branch on errors.Is. Storage layer surfaces sql.ErrNoRows as
// "no rows" via fmt.Errorf wrapping; conflict cases come back with
// "UNIQUE constraint" or similar.
func mapPipelineRepoErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrConflict) {
		return err
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "no rows") || strings.Contains(lower, "not found"):
		return fmt.Errorf("%w: %v", domain.ErrNotFound, err)
	case strings.Contains(lower, "unique") || strings.Contains(lower, "conflict"):
		return fmt.Errorf("%w: %v", domain.ErrConflict, err)
	default:
		return err
	}
}

// pipelineOpKind tells pipelineValidator which CRUD verb is in flight.
// kit/runtime/domain.Service exposes a single Validate(entity) signature;
// the discriminator pattern (action-aware via a sentinel context value)
// is the same workaround wsm uses until kit grows per-op validators.
type pipelineOpKind int

const (
	// pipelineOpCreate runs full create-time validation: name + steps
	// presence.
	pipelineOpCreate pipelineOpKind = iota + 1
	// pipelineOpArchive is an Update verb. Status flip is performed by
	// the manager before the service call; validator no-ops because the
	// pre-flip precondition can't be inferred from the post-flip entity.
	pipelineOpArchive
	// pipelineOpUnarchive is the inverse of Archive. Same rationale as
	// pipelineOpArchive: validator no-ops.
	pipelineOpUnarchive
)

// pipelineValidateContextKey carries the action discriminator into the
// validator. It does not flow over the bus — only inside the goroutine
// running the op.
type pipelineValidateContextKey struct{}

func withPipelineValidateOp(ctx context.Context, op pipelineOpKind) context.Context {
	return context.WithValue(ctx, pipelineValidateContextKey{}, op)
}

// pipelineValidator runs invariants on a Pipeline before persistence.
// It runs in domain.Service[storage.Pipeline]'s validation slot, between
// kit.runtime.entity.pre_validated and kit.runtime.entity.pre_persisted.
//
// Today the only invariant is: Name required for create. The legacy
// API allows empty Steps (parsePipelineSteps returns no error on
// empty arrays) so the validator does not enforce step count — that
// stays a per-step concern handled by pipeline.ValidateComposability
// during execution. Centralizing the name check here lets future
// entry points (gRPC, CLI, scripts) inherit the rule without
// duplicating the HTTP layer's substring guard.
type pipelineValidator struct{}

// Compile-time assertion: pipelineValidator implements
// domain.Validator[storage.Pipeline].
var _ domain.Validator[storage.Pipeline] = (*pipelineValidator)(nil)

func newPipelineValidator() *pipelineValidator { return &pipelineValidator{} }

func (v *pipelineValidator) Validate(ctx context.Context, p storage.Pipeline) error {
	op, _ := ctx.Value(pipelineValidateContextKey{}).(pipelineOpKind)
	if op != pipelineOpCreate {
		// Update / Archive / Unarchive: the manager owns precondition
		// checks against the pre-flip state. The validator only
		// enforces create-time rules so post-flip entities don't get
		// rejected for missing data that already passed at create time.
		return nil
	}
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("%w: pipeline name is required", domain.ErrValidation)
	}
	return nil
}

// newPipelineService wires a domain.Service[storage.Pipeline] around
// the supplied storage. The service uses kit's DefaultTopics
// (kit.runtime.entity.*) since ctxt has no pre-existing pipeline-event
// consumers expecting ctxt.runtime.pipeline.* topics — kit's canonical
// seam is the path of least surprise for future subscribers (policy
// engine, audit writers).
//
// No EventPublisher is attached today: ctxt's events.Bus is its own
// in-process interface and the cross-process kit bus instance lives
// only in dpkms serve. Wiring a publisher would expand surface beyond
// this refactor's scope. Pre-event seams stay no-ops until a publisher
// is attached; once attached, veto propagates with no further code
// changes here.
func newPipelineService(store storage.PipelineStore) *domain.Service[storage.Pipeline] {
	repo := newPipelineRepo(store)
	val := newPipelineValidator()
	opts := []domain.Option[storage.Pipeline]{
		domain.WithValidation[storage.Pipeline](val),
	}
	return domain.NewService[storage.Pipeline](repo, opts...)
}
