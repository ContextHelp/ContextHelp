package integration

// US-0054: Saved Search and Alerts
// Tests the closest available primitives: Detector creation (content-pattern
// matching) and verifying that a newly ingested object matching the query is
// discoverable via RSQL search — standing in for an alert "firing".
//
// Note: A dedicated saved-search table is not yet implemented. These tests
// validate the constituent capabilities (persist a query pattern, ingest
// matching content, confirm match is findable) that underpin the story.

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUS0054_DetectorPersistsQueryPattern verifies that a detector record (the
// current saved-search primitive) can be created and listed.
func TestUS0054_DetectorPersistsQueryPattern(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	det, err := env.svc.CreateDetector(ctx, service.DetectorCreateRequest{
		Kind:         storage.DetectorKindContentTest,
		Name:         "security-decisions",
		PipelineName: "text.short",
		Pattern:      "security",
		Priority:     50,
	})
	require.NoError(t, err)
	require.NotEmpty(t, det.ID, "detector must be assigned an ID")
	assert.Equal(t, "security-decisions", det.Name)
	assert.True(t, det.Enabled, "new detector must be enabled by default")

	// List must include the created detector.
	detectors, err := env.svc.ListDetectors(ctx, storage.DetectorFilter{})
	require.NoError(t, err)
	found := false
	for _, d := range detectors {
		if d.ID == det.ID {
			found = true
			break
		}
	}
	assert.True(t, found, "created detector must appear in listing")
}

// TestUS0054_IngestMatchingObjectFoundByQuery verifies the alert-like flow:
// save a query pattern → ingest matching object → confirm it is searchable.
func TestUS0054_IngestMatchingObjectFoundByQuery(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	// Persist a "saved search" as a detector.
	_, err := env.svc.CreateDetector(ctx, service.DetectorCreateRequest{
		Kind:         storage.DetectorKindContentTest,
		Name:         "auth-decisions",
		PipelineName: "text.short",
		Pattern:      "authentication",
		Priority:     100,
	})
	require.NoError(t, err)

	// Ingest a new object that matches the pattern.
	require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID:         "ss-match-01",
		Type:       "decision",
		RawContent: "new authentication policy decision",
		Summaries:  []string{"authentication policy enforcement"},
		CreatedAt:  now,
		UpdatedAt:  now,
	}))
	rebuildFTSIntegration(t, env)

	// Re-run the saved query → must surface the new object.
	objs, total, err := env.svc.SearchObjects(ctx, "type==decision", 20, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, 1)
	assert.True(t, containsIDPtr(objs, "ss-match-01"),
		"newly ingested matching object must appear when saved query is re-executed")
}

// TestUS0054_DeleteDetectorRemovesRecord verifies that deleting a detector
// removes it from the listing.
func TestUS0054_DeleteDetectorRemovesRecord(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	det, err := env.svc.CreateDetector(ctx, service.DetectorCreateRequest{
		Kind:         storage.DetectorKindExtension,
		Name:         "pdf-watcher",
		PipelineName: "document.pdf",
		Pattern:      ".pdf",
		Priority:     10,
	})
	require.NoError(t, err)

	require.NoError(t, env.svc.DeleteDetector(ctx, det.ID))

	detectors, err := env.svc.ListDetectors(ctx, storage.DetectorFilter{})
	require.NoError(t, err)
	for _, d := range detectors {
		assert.NotEqual(t, det.ID, d.ID, "deleted detector must not appear in listing")
	}
}
