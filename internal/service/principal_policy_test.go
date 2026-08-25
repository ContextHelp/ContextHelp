package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/kit/go/runtime/bus"
	kitpolicy "hop.top/kit/go/runtime/policy"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	ctxtpolicy "github.com/ideacrafterslabs/ctxt/internal/policy"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// principalPolicy denies pipeline deletes unless the request carries an
// authenticated principal, exercising the principal.* CEL binding end
// to end: transport middleware → ctx → domain seam → policy engine.
const principalPolicy = `policies:
  - name: delete-requires-principal
    on: kit.runtime.entity.pre_persisted
    when: 'payload.Op != "delete" || principal.id != ""'
    effect: allow
    otherwise: deny
    message: "deleting a pipeline requires an authenticated principal"
`

func newPrincipalPolicyService(t *testing.T) (*Service, func()) {
	t.Helper()
	dir := t.TempDir()
	policyFile := filepath.Join(dir, "policies.yaml")
	require.NoError(t, os.WriteFile(policyFile, []byte(principalPolicy), 0o600))
	t.Setenv("CTXT_POLICY_FILE", policyFile)

	// The kit resolver falls back to $KIT_POLICY_ROLE/$USER when ctx
	// carries no principal (local single-user mode). Clear both so an
	// anonymous request in this test resolves to an empty principal,
	// the way a remote unauthenticated caller must.
	t.Setenv("KIT_POLICY_ROLE", "")
	t.Setenv("USER", "")

	driver := storageutil.NewTestDriver(t)
	q := jobs.NewQueue(driver.Jobs())
	pipes := pipeline.DefaultRegistry()
	engine := search.NewEngine(driver)

	b := bus.New()
	pol, err := ctxtpolicy.Init(b)
	require.NoError(t, err)

	svc := NewWithOptions(driver, q, pipes, engine, "", nil,
		[]Option{WithPolicyPublisher(pol.Publisher())},
	)
	cleanup := func() {
		pol.Close()
		_ = b.Close(context.Background())
	}
	return svc, cleanup
}

func TestPolicy_PrincipalPropagatesIntoCEL(t *testing.T) {
	svc, cleanup := newPrincipalPolicyService(t)
	defer cleanup()

	ctx := context.Background()
	_, err := svc.CreatePipeline(ctx, CreatePipelineRequest{
		Name:  "principal-gated",
		Steps: `[{"name":"text_summary"}]`,
	})
	require.NoError(t, err)

	// Anonymous delete → the principal.id != "" leg fails → denied.
	err = svc.DeletePipeline(ctx, "principal-gated")
	require.Error(t, err)
	var pde *kitpolicy.PolicyDeniedError
	require.True(t, errors.As(err, &pde), "expected PolicyDeniedError, got %T: %v", err, err)
	assert.Equal(t, "delete-requires-principal", pde.PolicyName)

	// Same op with an authenticated principal attached the way the
	// transport middleware does it → allowed.
	authed := authn.Attach(ctx, &authn.Principal{
		ID:       "ops",
		Provider: authn.ProviderStatic,
		Roles:    []string{"admin"},
	})
	require.NoError(t, svc.DeletePipeline(authed, "principal-gated"))
}
