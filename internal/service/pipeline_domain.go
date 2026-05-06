package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"hop.top/kit/go/runtime/domain"
	"hop.top/kit/go/runtime/policy"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// withPolicyAction merges an "action" attribute into the
// policy.ContextAttrsKey map without losing keys already attached
// upstream (notably "note" stuffed by the HTTP layer). The CEL rule
// archive-pipeline-requires-note reads context.request_attrs.action
// to differentiate archive from unarchive, since both share the same
// Op="update" topic. kit's contextAttrsFromCtx forwards "note" and
// "request_attrs" verbatim — anything outside that allowlist is
// dropped, so action must live under request_attrs.
func withPolicyAction(ctx context.Context, action string) context.Context {
	existing, _ := ctx.Value(policy.ContextAttrsKey).(map[string]any)
	merged := make(map[string]any, len(existing)+1)
	for k, v := range existing {
		merged[k] = v
	}
	reqAttrs, _ := merged["request_attrs"].(map[string]any)
	if reqAttrs == nil {
		reqAttrs = map[string]any{}
	} else {
		copy := make(map[string]any, len(reqAttrs)+1)
		for k, v := range reqAttrs {
			copy[k] = v
		}
		reqAttrs = copy
	}
	reqAttrs["action"] = action
	merged["request_attrs"] = reqAttrs
	return context.WithValue(ctx, policy.ContextAttrsKey, merged)
}

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
//
// Op discrimination uses kit's canonical domain.OpFromCtx — Service
// auto-injects domain.OpCreate/OpUpdate/OpDelete. Update verbs
// (archive, unarchive) skip create-time invariants so post-flip
// entities don't get rejected for data that already passed at
// create time.
type pipelineValidator struct{}

// Compile-time assertion: pipelineValidator implements
// domain.Validator[storage.Pipeline].
var _ domain.Validator[storage.Pipeline] = (*pipelineValidator)(nil)

func newPipelineValidator() *pipelineValidator { return &pipelineValidator{} }

func (v *pipelineValidator) Validate(ctx context.Context, p storage.Pipeline) error {
	if domain.OpFromCtx(ctx) != domain.OpCreate {
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
// pub may be nil. Daemons (dpkms serve) construct a kit/bus-backed
// publisher and pass it; tests that need only the repo+validator
// path leave it nil. When pub is non-nil, every Create/Update/Delete
// fires kit.runtime.entity.pre_persisted on the bus and a sync
// subscriber (the policy engine) can veto by returning an error.
func newPipelineService(store storage.PipelineStore, pub domain.EventPublisher) *domain.Service[storage.Pipeline] {
	repo := newPipelineRepo(store)
	val := newPipelineValidator()
	opts := []domain.Option[storage.Pipeline]{
		domain.WithValidation[storage.Pipeline](val),
	}
	if pub != nil {
		opts = append(opts, domain.WithPublisher[storage.Pipeline](pub))
	}
	return domain.NewService[storage.Pipeline](repo, opts...)
}
