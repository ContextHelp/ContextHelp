package policy_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/kit/go/runtime/bus"
	"hop.top/kit/go/runtime/domain"
	kitpolicy "hop.top/kit/go/runtime/policy"

	ctxtpolicy "github.com/ideacrafterslabs/ctxt/internal/policy"
)

// fakeAdapterEntity is a minimal domain.Entity standing in for an
// adapter-managed object (e.g. an email message, a contact card, an
// RSS item). Its presence in the test proves the policy gate works
// regardless of the concrete adapter type — domain.Service[T] is
// generic over T.
type fakeAdapterEntity struct {
	ID   string
	Kind string
}

func (e fakeAdapterEntity) GetID() string { return e.ID }

// fakeAdapterRepo is the minimum domain.Repository[T] needed to
// exercise domain.Service.Create end-to-end. Adapter-driven mutations
// in Phase 2 will use real repositories; this stub is just enough to
// route a Create through the pre-event seam where the policy engine
// vetoes.
type fakeAdapterRepo struct {
	created []fakeAdapterEntity
}

func (r *fakeAdapterRepo) Create(_ context.Context, e *fakeAdapterEntity) error {
	r.created = append(r.created, *e)
	return nil
}
func (r *fakeAdapterRepo) Get(_ context.Context, id string) (*fakeAdapterEntity, error) {
	for i := range r.created {
		if r.created[i].ID == id {
			return &r.created[i], nil
		}
	}
	return nil, domain.ErrNotFound
}
func (r *fakeAdapterRepo) List(_ context.Context, _ domain.Query) ([]fakeAdapterEntity, error) {
	out := make([]fakeAdapterEntity, len(r.created))
	copy(out, r.created)
	return out, nil
}
func (r *fakeAdapterRepo) Update(_ context.Context, _ *fakeAdapterEntity) error { return nil }
func (r *fakeAdapterRepo) Delete(_ context.Context, _ string) error             { return nil }

// TestGate4_AdapterMutationVetoedByPolicy is the Acceptance Gate 4
// proof for adapter-driven entity mutations. An adapter constructs a
// domain.Service[T] using the daemon's policy.Bootstrap.Publisher()
// and attempts a Create. A CEL rule on kit.runtime.entity.pre_persisted
// — the topic the substrate already wires — vetoes the mutation.
//
// This proves the substrate doesn't need a parallel adapter-namespaced
// veto-able topic family: adapter mutations inherit the kit gate that
// PR #23 wired for pipeline ops, as long as the adapter routes through
// domain.Service[T]. CEL rules discriminate adapters from pipelines
// (or one adapter from another) via payload.kind / resource.kind /
// resource.fields.* clauses — see ADR-065 §Amendment 2026-05-06
// §Acceptance Gate 4.
func TestGate4_AdapterMutationVetoedByPolicy(t *testing.T) {
	dir := t.TempDir()
	policyFile := filepath.Join(dir, "ctxt.yaml")
	require.NoError(t, writeFile(policyFile, []byte(`policies:
  - name: deny-adapter-create
    on: kit.runtime.entity.pre_persisted
    when: 'payload.Op != "create"'
    effect: allow
    otherwise: deny
    message: "test rule denies all adapter-driven create"
`)))
	t.Setenv("CTXT_POLICY_FILE", policyFile)

	b := bus.New()
	t.Cleanup(func() { _ = b.Close(context.Background()) })

	pol, err := ctxtpolicy.Init(b)
	require.NoError(t, err)
	t.Cleanup(pol.Close)

	// An adapter owns a repo + a domain.Service over its entity type,
	// passing the policy bootstrap's publisher. Same wiring pattern
	// the pipeline manager uses (PR #23, internal/service/service.go).
	repo := &fakeAdapterRepo{}
	svc := domain.NewService[fakeAdapterEntity](repo,
		domain.WithPublisher[fakeAdapterEntity](pol.Publisher()),
	)

	// Adapter attempts to persist an entity. The CEL rule on
	// pre_persisted vetoes; Create returns the policy error and the
	// repo never sees the entity.
	err = svc.Create(context.Background(), &fakeAdapterEntity{ID: "msg-1", Kind: "email"})
	require.Error(t, err)
	var pde *kitpolicy.PolicyDeniedError
	require.True(t, errors.As(err, &pde), "expected PolicyDeniedError, got %T: %v", err, err)
	assert.Equal(t, "deny-adapter-create", pde.PolicyName)
	assert.Empty(t, repo.created, "veto must abort before repo.Create runs")
}

// TestGate4_AdapterMutationAllowedByPolicy is the inverse: when no
// rule matches, the mutation proceeds. Sanity check that the substrate
// doesn't accidentally veto everything routed through domain.Service.
func TestGate4_AdapterMutationAllowedByPolicy(t *testing.T) {
	dir := t.TempDir()
	policyFile := filepath.Join(dir, "ctxt.yaml")
	require.NoError(t, writeFile(policyFile, []byte("policies: []\n")))
	t.Setenv("CTXT_POLICY_FILE", policyFile)

	b := bus.New()
	t.Cleanup(func() { _ = b.Close(context.Background()) })

	pol, err := ctxtpolicy.Init(b)
	require.NoError(t, err)
	t.Cleanup(pol.Close)

	repo := &fakeAdapterRepo{}
	svc := domain.NewService[fakeAdapterEntity](repo,
		domain.WithPublisher[fakeAdapterEntity](pol.Publisher()),
	)

	err = svc.Create(context.Background(), &fakeAdapterEntity{ID: "msg-1", Kind: "email"})
	require.NoError(t, err)
	assert.Len(t, repo.created, 1)
	assert.Equal(t, "msg-1", repo.created[0].ID)
}

// TestGate4_PayloadDiscriminatesAdapterFromPipeline proves the
// per-adapter discrimination convention from the ADR amendment: a CEL
// rule reading payload.kind (or resource.fields.*) can target one
// adapter's mutations without affecting another's. This replaces the
// per-protocol `on: dpkms.<protocol>.entity.pre_persisted` topic
// targeting that kit's allowlist rejects.
func TestGate4_PayloadDiscriminatesAdapterFromPipeline(t *testing.T) {
	dir := t.TempDir()
	policyFile := filepath.Join(dir, "ctxt.yaml")
	// Deny only when the entity has Kind == "email"; everything else
	// passes. Mirrors how Phase 2 adapters will gate themselves.
	require.NoError(t, writeFile(policyFile, []byte(`policies:
  - name: deny-email-only
    on: kit.runtime.entity.pre_persisted
    when: 'resource.fields.Kind != "email"'
    effect: allow
    otherwise: deny
    message: "test rule denies email entities only"
`)))
	t.Setenv("CTXT_POLICY_FILE", policyFile)

	b := bus.New()
	t.Cleanup(func() { _ = b.Close(context.Background()) })

	pol, err := ctxtpolicy.Init(b)
	require.NoError(t, err)
	t.Cleanup(pol.Close)

	repo := &fakeAdapterRepo{}
	svc := domain.NewService[fakeAdapterEntity](repo,
		domain.WithPublisher[fakeAdapterEntity](pol.Publisher()),
	)

	// email Kind → vetoed.
	err = svc.Create(context.Background(), &fakeAdapterEntity{ID: "msg-1", Kind: "email"})
	require.Error(t, err)
	var pde *kitpolicy.PolicyDeniedError
	require.True(t, errors.As(err, &pde))
	assert.Equal(t, "deny-email-only", pde.PolicyName)

	// contacts Kind → allowed.
	require.NoError(t, svc.Create(context.Background(), &fakeAdapterEntity{ID: "card-1", Kind: "contacts"}))
	assert.Len(t, repo.created, 1, "only the contacts entity should have been persisted")
}
