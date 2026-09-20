package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// stubStepStore implements storage.StepStore for tests.
type stubStepStore struct {
	step *storage.RegisteredStep
}

func (s *stubStepStore) Create(_ context.Context, _ *storage.RegisteredStep) error { return nil }
func (s *stubStepStore) Get(_ context.Context, _ string) (*storage.RegisteredStep, error) {
	return s.step, nil
}
func (s *stubStepStore) List(_ context.Context, _ string) ([]*storage.RegisteredStep, int, error) {
	if s.step != nil {
		return []*storage.RegisteredStep{s.step}, 1, nil
	}
	return nil, 0, nil
}
func (s *stubStepStore) Unregister(_ context.Context, _ string) error              { return nil }
func (s *stubStepStore) Update(_ context.Context, _ *storage.RegisteredStep) error { return nil }

// stubDriver implements just enough of storage.StorageDriver for executor tests.
type stubDriver struct {
	steps *stubStepStore
}

func (d *stubDriver) Init(_ context.Context) error               { return nil }
func (d *stubDriver) Close(_ context.Context) error              { return nil }
func (d *stubDriver) Steps() storage.StepStore                   { return d.steps }
func (d *stubDriver) Objects() storage.ObjectStore               { return nil }
func (d *stubDriver) Entities() storage.EntityStore              { return nil }
func (d *stubDriver) Edges() storage.EdgeStore                   { return nil }
func (d *stubDriver) Jobs() storage.JobStore                     { return nil }
func (d *stubDriver) Pipelines() storage.PipelineStore           { return nil }
func (d *stubDriver) Registries() storage.RegistryStore          { return nil }
func (d *stubDriver) Reminders() storage.ReminderStore           { return nil }
func (d *stubDriver) Feeds() storage.FeedStore                   { return nil }
func (d *stubDriver) FeedItems() storage.FeedItemStore           { return nil }
func (d *stubDriver) Batches() storage.BatchStore                { return nil }
func (d *stubDriver) Detectors() storage.DetectorStore           { return nil }
func (d *stubDriver) Blobs() storage.BlobStore                   { return nil }
func (d *stubDriver) Proximity() storage.ProximityStore          { return nil }
func (d *stubDriver) Watches() storage.WatchStore                { return nil }
func (d *stubDriver) Aliases() storage.AliasStore                { return nil }
func (d *stubDriver) AuditLog() storage.AuditStore               { return nil }
func (d *stubDriver) Attachments() storage.AttachmentStore       { return nil }
func (d *stubDriver) Resurfacing() storage.ResurfacingQueueStore { return nil }
func (d *stubDriver) Entitlements() storage.EntitlementStore     { return nil }
func (d *stubDriver) Metering() storage.MeteringStore            { return nil }
func (d *stubDriver) Vectors() storage.VectorStore               { return nil }
func (d *stubDriver) SavedSearches() storage.SavedSearchStore    { return nil }
func (d *stubDriver) SearchHistory() storage.SearchHistoryStore  { return nil }
func (d *stubDriver) Watermarks() storage.WatermarkStore         { return nil }
func (d *stubDriver) Health(_ context.Context) error             { return nil }

// buildPayload encodes a KnowledgeObject as the `object` field of an
// envelope that a well-behaved external step would emit.
func buildPayload(t *testing.T, obj *storage.KnowledgeObject) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"object": obj})
	require.NoError(t, err)
	return string(b)
}

// TestExecuteWithProcess_JSONObjectPayload verifies that process execution
// correctly decodes a normal JSON object returned by an external step.
func TestExecuteWithProcess_JSONObjectPayload(t *testing.T) {
	want := &storage.KnowledgeObject{
		ID:         "test-id-001",
		Type:       "note",
		RawContent: "hello from step",
	}

	payload := buildPayload(t, want)

	// The script lives at <dir>/bin/step so getStepExecutablePath finds it.
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	scriptPath := filepath.Join(binDir, "step")
	script := fmt.Sprintf("#!/bin/sh\necho '%s'\n", payload)
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))

	step := &storage.RegisteredStep{
		Name:   "test-step",
		Source: "local",
		Path:   dir,
	}

	driver := &stubDriver{steps: &stubStepStore{step: step}}
	se := NewStepExecutor(driver, dir)
	se.RegisterBuiltin("test-step", &ExternalStep{name: "test-step", path: dir})

	sandbox := &storage.SandboxConfig{
		Enabled:        true,
		IsolationLevel: "process",
	}

	draft := &storage.KnowledgeObject{ID: "draft-001", Type: "note"}
	got, err := se.ExecuteStep(context.Background(), "test-step", nil, sandbox, draft)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want.ID, got.ID)
	assert.Equal(t, want.Type, got.Type)
	assert.Equal(t, want.RawContent, got.RawContent)
}

// TestExecuteWithProcess_ErrorPayload verifies that a step-reported error is
// surfaced correctly.
func TestExecuteWithProcess_ErrorPayload(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	scriptPath := filepath.Join(binDir, "step")
	script := "#!/bin/sh\necho '{\"error\":\"step failed: bad input\"}'\n"
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))
	_ = scriptPath

	step := &storage.RegisteredStep{
		Name:   "error-step",
		Source: "local",
		Path:   dir,
	}

	driver := &stubDriver{steps: &stubStepStore{step: step}}
	se := NewStepExecutor(driver, dir)
	se.RegisterBuiltin("error-step", &ExternalStep{name: "error-step", path: dir})

	sandbox := &storage.SandboxConfig{
		Enabled:        true,
		IsolationLevel: "process",
	}

	_, err := se.ExecuteStep(context.Background(), "error-step", nil, sandbox,
		&storage.KnowledgeObject{ID: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "step failed: bad input")
}

// TestExecuteWithProcess_MissingObjectField verifies the "missing object field"
// error path is returned when the step omits the object key.
func TestExecuteWithProcess_MissingObjectField(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	scriptPath := filepath.Join(binDir, "step")
	script := "#!/bin/sh\necho '{\"status\":\"ok\"}'\n"
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))
	_ = scriptPath

	step := &storage.RegisteredStep{
		Name:   "noobj-step",
		Source: "local",
		Path:   dir,
	}

	driver := &stubDriver{steps: &stubStepStore{step: step}}
	se := NewStepExecutor(driver, dir)
	se.RegisterBuiltin("noobj-step", &ExternalStep{name: "noobj-step", path: dir})

	sandbox := &storage.SandboxConfig{
		Enabled:        true,
		IsolationLevel: "process",
	}

	_, err := se.ExecuteStep(context.Background(), "noobj-step", nil, sandbox,
		&storage.KnowledgeObject{ID: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing object field")
}
