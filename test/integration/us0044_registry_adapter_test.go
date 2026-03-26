package integration

// US-0044: Implement Registry Adapter Plugin
// Verifies AliasResolver plugin: Init accesses alias store, Create persists alias,
// ResolveID returns canonical ID, unknown aliases pass through unchanged.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/plugin"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// ── Stub registry adapter plugin ─────────────────────────────────────────────

type stubRegistryAdapterPlugin struct {
	store      pluginapi.AliasStore
	initCalled bool
}

func (p *stubRegistryAdapterPlugin) Name() string    { return "stub-registry-adapter" }
func (p *stubRegistryAdapterPlugin) Version() string { return "1.0.0" }

func (p *stubRegistryAdapterPlugin) Init(
	_ context.Context, _ map[string]interface{}, deps pluginapi.Deps,
) error {
	p.initCalled = true
	if deps.Store == nil {
		return errors.New("stub-registry-adapter: store not injected")
	}
	p.store = deps.Store.Aliases()
	return nil
}

func (p *stubRegistryAdapterPlugin) PipelineSteps() []pluginapi.PipelineStep { return nil }
func (p *stubRegistryAdapterPlugin) Close(_ context.Context) error           { return nil }

// ResolveID implements pluginapi.AliasResolver.
func (p *stubRegistryAdapterPlugin) ResolveID(
	ctx context.Context, idOrAlias, profile string,
) (string, error) {
	if p.store == nil {
		return idOrAlias, nil
	}
	id, err := p.store.Resolve(ctx, idOrAlias, profile)
	if err != nil {
		return idOrAlias, nil // not an alias; pass through
	}
	return id, nil
}

// Compile-time check: satisfies AliasResolver.
var _ pluginapi.AliasResolver = (*stubRegistryAdapterPlugin)(nil)

// ── helpers ───────────────────────────────────────────────────────────────────

// seedObject creates a minimal KnowledgeObject in driver so alias FK succeeds.
func seedObject(t *testing.T, driver storage.StorageDriver, id string) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	obj := &storage.KnowledgeObject{
		ID:         id,
		Type:       "text",
		RawContent: "seed content for alias test",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	require.NoError(t, driver.Objects().Create(context.Background(), obj))
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestUS0044_PluginRegisteredAsAliasResolver verifies registry lists it.
func TestUS0044_PluginRegisteredAsAliasResolver(t *testing.T) {
	reg := plugin.NewRegistry()
	pl := &stubRegistryAdapterPlugin{}
	reg.Register(pl)

	resolvers := reg.AliasResolvers()
	require.Len(t, resolvers, 1, "one AliasResolver must be registered")
	assert.Equal(t, "stub-registry-adapter", resolvers[0].Name())
}

// TestUS0044_InitWithNilStoreReturnsError verifies error when store not injected.
func TestUS0044_InitWithNilStoreReturnsError(t *testing.T) {
	pl := &stubRegistryAdapterPlugin{}
	err := pl.Init(context.Background(), nil, pluginapi.Deps{Store: nil})
	require.Error(t, err, "Init with nil store must return error")
	assert.Contains(t, err.Error(), "store not injected")
}

// TestUS0044_AliasCreationAndResolution verifies Create + ResolveID round-trip.
func TestUS0044_AliasCreationAndResolution(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	seedObject(t, driver, "uuid-001")

	pl := &stubRegistryAdapterPlugin{}
	deps := pluginapi.Deps{Store: driver}
	err := pl.Init(context.Background(), map[string]interface{}{}, deps)
	require.NoError(t, err)
	assert.True(t, pl.initCalled)

	ctx := context.Background()
	now := time.Now()

	// Create a global alias pointing to the seeded object.
	alias := &pluginapi.Alias{
		Alias:     "my-project",
		ObjectID:  "uuid-001",
		Scope:     "global",
		Profile:   "",
		CreatedAt: now,
		UpdatedAt: now,
	}
	err = pl.store.Create(ctx, alias)
	require.NoError(t, err)

	// ResolveID returns the canonical object ID.
	resolved, err := pl.ResolveID(ctx, "my-project", "")
	require.NoError(t, err)
	assert.Equal(t, "uuid-001", resolved, "global alias must resolve to object ID")
}

// TestUS0044_UnknownAliasPassesThrough verifies unknown alias returned unchanged.
func TestUS0044_UnknownAliasPassesThrough(t *testing.T) {
	driver := storageutil.NewTestDriver(t)

	pl := &stubRegistryAdapterPlugin{}
	err := pl.Init(context.Background(), nil, pluginapi.Deps{Store: driver})
	require.NoError(t, err)

	ctx := context.Background()
	result, err := pl.ResolveID(ctx, "unknown-alias", "")
	require.NoError(t, err)
	assert.Equal(t, "unknown-alias", result, "unknown alias must be returned unchanged")
}

// TestUS0044_ProfileScopedAlias verifies profile-scoped resolution.
func TestUS0044_ProfileScopedAlias(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	seedObject(t, driver, "uuid-sprint-42")

	pl := &stubRegistryAdapterPlugin{}
	err := pl.Init(context.Background(), nil, pluginapi.Deps{Store: driver})
	require.NoError(t, err)

	ctx := context.Background()
	now := time.Now()

	err = pl.store.Create(ctx, &pluginapi.Alias{
		Alias:     "sprint-42",
		ObjectID:  "uuid-sprint-42",
		Scope:     "profile",
		Profile:   "dev",
		CreatedAt: now,
		UpdatedAt: now,
	})
	require.NoError(t, err)

	// Resolves under matching profile.
	id, err := pl.ResolveID(ctx, "sprint-42", "dev")
	require.NoError(t, err)
	assert.Equal(t, "uuid-sprint-42", id)

	// Does NOT resolve under different profile.
	id2, err := pl.ResolveID(ctx, "sprint-42", "prod")
	require.NoError(t, err)
	assert.Equal(t, "sprint-42", id2, "wrong profile must return alias unchanged")
}

// TestUS0044_GlobalAliasResolvesForAnyProfile verifies global scope ignores profile.
func TestUS0044_GlobalAliasResolvesForAnyProfile(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	seedObject(t, driver, "uuid-shared")

	pl := &stubRegistryAdapterPlugin{}
	err := pl.Init(context.Background(), nil, pluginapi.Deps{Store: driver})
	require.NoError(t, err)

	ctx := context.Background()
	now := time.Now()

	err = pl.store.Create(ctx, &pluginapi.Alias{
		Alias:     "shared-alias",
		ObjectID:  "uuid-shared",
		Scope:     "global",
		Profile:   "",
		CreatedAt: now,
		UpdatedAt: now,
	})
	require.NoError(t, err)

	// Global alias resolves regardless of profile.
	id, err := pl.ResolveID(ctx, "shared-alias", "any-profile")
	require.NoError(t, err)
	assert.Equal(t, "uuid-shared", id)
}

// TestUS0044_RegistryResolveIDFirstMatchWins verifies first matching resolver wins.
func TestUS0044_RegistryResolveIDFirstMatchWins(t *testing.T) {
	// pl1 uses driver1 — no alias "x" registered there.
	driver1 := storageutil.NewTestDriver(t)
	pl1 := &stubRegistryAdapterPlugin{}
	err := pl1.Init(context.Background(), nil, pluginapi.Deps{Store: driver1})
	require.NoError(t, err)

	// pl2 uses driver2 — alias "x" registered there.
	driver2 := storageutil.NewTestDriver(t)
	seedObject(t, driver2, "uuid-x")
	pl2 := &stubRegistryAdapterPlugin{}
	err = pl2.Init(context.Background(), nil, pluginapi.Deps{Store: driver2})
	require.NoError(t, err)

	// Wrap pl2 under a different plugin name so both can be registered.
	pl2Named := &namedAliasPlugin{
		inner: pl2,
		name:  "stub-registry-adapter-2",
	}

	ctx := context.Background()
	now := time.Now()

	err = driver2.Aliases().Create(ctx, &pluginapi.Alias{
		Alias:     "x",
		ObjectID:  "uuid-x",
		Scope:     "global",
		Profile:   "",
		CreatedAt: now,
		UpdatedAt: now,
	})
	require.NoError(t, err)

	reg := plugin.NewRegistry()
	reg.Register(pl1)
	reg.Register(pl2Named)

	// Registry.ResolveID tries pl1 first (no match), then pl2 (match).
	resolved, err := reg.ResolveID(ctx, "x", "")
	require.NoError(t, err)
	assert.Equal(t, "uuid-x", resolved, "second resolver must win when first has no match")
}

// namedAliasPlugin wraps a stubRegistryAdapterPlugin with a custom name,
// delegating ResolveID to the inner plugin's store.
type namedAliasPlugin struct {
	inner *stubRegistryAdapterPlugin
	name  string
}

func (p *namedAliasPlugin) Name() string    { return p.name }
func (p *namedAliasPlugin) Version() string { return "1.0.0" }
func (p *namedAliasPlugin) Init(_ context.Context, _ map[string]interface{}, _ pluginapi.Deps) error {
	return nil
}
func (p *namedAliasPlugin) PipelineSteps() []pluginapi.PipelineStep { return nil }
func (p *namedAliasPlugin) Close(_ context.Context) error           { return nil }
func (p *namedAliasPlugin) ResolveID(ctx context.Context, idOrAlias, profile string) (string, error) {
	return p.inner.ResolveID(ctx, idOrAlias, profile)
}

var _ pluginapi.AliasResolver = (*namedAliasPlugin)(nil)
