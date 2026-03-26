package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// US-0030 Tests — Focus Profiles
//
// Focus profiles are a client-side config concept stored in config.yaml.
// The server receives profile name in search queries to scope results.
//
// These tests verify:
//   1. FocusProfile config struct fields round-trip through JSON.
//   2. ProfileConfig.Default and Profiles map round-trip correctly.
//   3. Objects created with ProfileID are retrievable and filtered by profile.
//   4. Search scoped via ProfileID query param filters to the right objects.
// ---------------------------------------------------------------------------

// TestUS0030_ProfileConfigRoundTrip verifies FocusProfile fields survive
// JSON marshal/unmarshal without data loss.
func TestUS0030_ProfileConfigRoundTrip(t *testing.T) {
	original := config.FocusProfile{
		Description:       "Startup founder lens",
		Tags:              []string{"strategy", "growth"},
		MentionNamespaces: []string{"investor", "company"},
		RerankBoosts: map[string]float64{
			"decision": 1.5,
			"entity":   1.2,
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored config.FocusProfile
	require.NoError(t, json.Unmarshal(data, &restored))

	assert.Equal(t, original.Description, restored.Description)
	assert.Equal(t, original.Tags, restored.Tags)
	assert.Equal(t, original.MentionNamespaces, restored.MentionNamespaces)
	assert.InDelta(t, original.RerankBoosts["decision"], restored.RerankBoosts["decision"], 0.0001)
	assert.InDelta(t, original.RerankBoosts["entity"], restored.RerankBoosts["entity"], 0.0001)
}

// TestUS0030_ProfileConfigDefaultRoundTrip verifies ProfileConfig.Default field
// and profile map survive JSON marshal/unmarshal.
func TestUS0030_ProfileConfigDefaultRoundTrip(t *testing.T) {
	original := config.ProfileConfig{
		Default: "founder",
		Profiles: map[string]config.FocusProfile{
			"founder": {
				Description: "Startup founder lens",
				Tags:        []string{"strategy"},
			},
			"engineer": {
				Description: "Engineering deep-dives",
				Tags:        []string{"code", "architecture"},
			},
		},
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored config.ProfileConfig
	require.NoError(t, json.Unmarshal(data, &restored))

	assert.Equal(t, "founder", restored.Default)
	require.Len(t, restored.Profiles, 2)
	assert.Equal(t, "Startup founder lens", restored.Profiles["founder"].Description)
	assert.Equal(t, "Engineering deep-dives", restored.Profiles["engineer"].Description)
}

// TestUS0030_ObjectWithProfileIDStoredAndRetrievable verifies that a KnowledgeObject
// created directly with a ProfileID is retrievable and has the correct ProfileID.
func TestUS0030_ObjectWithProfileIDStoredAndRetrievable(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	obj := &storage.KnowledgeObject{
		ID:        "profile-obj-001",
		Type:      "text",
		ProfileID: "founder",
		CreatedAt: now,
		UpdatedAt: now,
	}
	err := env.svc.Store.Objects().Create(ctx, obj)
	require.NoError(t, err)

	got, err := env.svc.Store.Objects().Get(ctx, "profile-obj-001")
	require.NoError(t, err)

	assert.Equal(t, "founder", got.ProfileID, "ProfileID must be stored and retrieved")
}

// TestUS0030_EmptyProfileIDIsGlobal verifies that a KnowledgeObject created without
// a ProfileID has an empty ProfileID (global scope).
func TestUS0030_EmptyProfileIDIsGlobal(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	obj := &storage.KnowledgeObject{
		ID:        "global-obj-001",
		Type:      "text",
		CreatedAt: now,
		UpdatedAt: now,
	}
	err := env.svc.Store.Objects().Create(ctx, obj)
	require.NoError(t, err)

	got, err := env.svc.Store.Objects().Get(ctx, "global-obj-001")
	require.NoError(t, err)

	assert.Empty(t, got.ProfileID, "object without profile must have empty ProfileID")
}

// TestUS0030_SearchFilteredByProfileReturnsCorrectObjects verifies that search
// scoped to a ProfileID returns only objects with that profile, not global ones.
func TestUS0030_SearchFilteredByProfileReturnsCorrectObjects(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	// Seed a profile-scoped object and a global object.
	founderObj := &storage.KnowledgeObject{
		ID:        "search-founder-obj",
		Type:      "article",
		ProfileID: "founder",
		CreatedAt: now,
		UpdatedAt: now,
	}
	globalObj := &storage.KnowledgeObject{
		ID:        "search-global-obj",
		Type:      "article",
		CreatedAt: now,
		UpdatedAt: now,
	}
	require.NoError(t, env.svc.Store.Objects().Create(ctx, founderObj))
	require.NoError(t, env.svc.Store.Objects().Create(ctx, globalObj))

	// SearchObjects with profileID="founder" should return only founder-scoped objects.
	results, total, err := env.svc.SearchObjects(ctx, "type==article", 100, 0, "founder")
	require.NoError(t, err)

	assert.GreaterOrEqual(t, total, 1, "at least one founder-scoped object expected")

	for _, r := range results {
		assert.Equal(t, "founder", r.ProfileID,
			"all results must belong to the founder profile, got ProfileID=%q", r.ProfileID)
	}
}

// TestUS0030_MultipleProfilesCoexistIndependently verifies that objects from
// different profiles are stored independently and each has the correct ProfileID.
func TestUS0030_MultipleProfilesCoexistIndependently(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now().Truncate(time.Second)

	profiles := []string{"founder", "engineer", "sales"}
	for i, prof := range profiles {
		obj := &storage.KnowledgeObject{
			ID:        "multi-profile-obj-" + prof,
			Type:      "text",
			ProfileID: prof,
			Source:    "e2e-test",
			CreatedAt: now.Add(time.Duration(i) * time.Second),
			UpdatedAt: now.Add(time.Duration(i) * time.Second),
		}
		require.NoError(t, env.svc.Store.Objects().Create(ctx, obj), "create for profile %s", prof)
	}

	for _, prof := range profiles {
		got, err := env.svc.Store.Objects().Get(ctx, "multi-profile-obj-"+prof)
		require.NoError(t, err, "get for profile %s", prof)
		assert.Equal(t, prof, got.ProfileID, "object for profile %s must have matching ProfileID", prof)
	}
}
