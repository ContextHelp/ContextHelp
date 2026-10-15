package stack

import (
	"context"
	"strings"
	"testing"
	"time"

	kitbus "hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func buildWithDriver(cfg *config.Config, driver storage.StorageDriver) (*Stack, error) {
	stubProviders(cfg)
	return Build(Inputs{Config: cfg, Driver: driver, PolicyBus: kitbus.New()})
}

func insertCfg(step string) *config.Config {
	cfg := &config.Config{}
	cfg.Pipelines.Overrides = map[string]config.PipelineOverride{
		"text.short": {InsertSteps: []config.StepInsert{{Step: step, Before: "tagger"}}},
	}
	return cfg
}

// An override naming a step that is not installed fails the build with a
// message naming the field; it never panics.
func TestBuildRefusesUnknownInsertedStep(t *testing.T) {
	st, err := buildWithDriver(insertCfg("pii-mask"), storageutil.NewTestDriver(t))
	if err == nil {
		st.Close()
		t.Fatal("Build: want error for an uninstalled step")
	}
	if !strings.Contains(err.Error(), `pipelines.overrides.text.short.insert_steps[0].step: unknown step "pii-mask"`) {
		t.Errorf("error: %v", err)
	}
}

// An installed step is resolvable like a built-in: the pipeline carries it
// right before its anchor.
func TestBuildResolvesInstalledStep(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	now := time.Now()
	if err := driver.Steps().Create(context.Background(), &storage.RegisteredStep{
		Name: "pii-mask", Source: "local", Path: t.TempDir(), InstalledAt: now, UpdatedAt: now,
		Metadata: &storage.StepMetadata{Name: "pii-mask", Execution: &storage.StepExecution{
			Type: "external", Executable: "pii-mask", ExecutableSHA256: strings.Repeat("0", 64),
		}},
	}); err != nil {
		t.Fatal(err)
	}
	st, err := buildWithDriver(insertCfg("pii-mask"), driver)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer st.Close()
	p, err := st.Pipelines.Get("text.short")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range p.Steps {
		names = append(names, s.Name())
	}
	if len(names) < 3 || names[1] != "pii-mask" || names[2] != "tagger" {
		t.Errorf("steps %v: want pii-mask right before tagger", names)
	}
}
