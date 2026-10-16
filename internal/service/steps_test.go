package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// A step pipelines.overrides still inserts cannot be uninstalled.
func TestUninstallStepRefusesStepInUse(t *testing.T) {
	ctx := ownerCtx()
	svc := newTestService(t)
	now := time.Now()
	if err := svc.store.Steps().Create(ctx, &storage.RegisteredStep{
		Name: "pii-mask", Source: "local", InstalledAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	svc.Cfg.Pipelines.Overrides = map[string]config.PipelineOverride{
		"text.short": {InsertSteps: []config.StepInsert{{Step: "pii-mask"}}},
	}

	err := svc.UninstallStep(ctx, "pii-mask")
	if !errors.Is(err, ErrStepInUse) || !strings.Contains(err.Error(), "pipelines.overrides.text.short.insert_steps[0]") {
		t.Fatalf("UninstallStep: %v; want ErrStepInUse naming the override", err)
	}
	if _, err := svc.store.Steps().Get(ctx, "pii-mask"); err != nil {
		t.Errorf("step record gone after a refused uninstall: %v", err)
	}

	svc.Cfg.Pipelines.Overrides = nil
	if err := svc.UninstallStep(ctx, "pii-mask"); err != nil {
		t.Fatalf("UninstallStep: %v", err)
	}
	if err := svc.UninstallStep(ctx, "pii-mask"); !errors.Is(err, steps.ErrStepNotFound) {
		t.Errorf("second uninstall: %v; want ErrStepNotFound", err)
	}
}

func TestInstallStepNeedsOneSource(t *testing.T) {
	svc := newTestService(t)
	for _, req := range []StepInstallRequest{
		{Name: "x"},
		{Name: "x", FromRegistry: "http://127.0.0.1:1", Archive: []byte("a")},
	} {
		if _, err := svc.InstallStep(ownerCtx(), req); !errors.Is(err, steps.ErrInvalidStep) {
			t.Errorf("InstallStep(%+v): %v; want ErrInvalidStep", req, err)
		}
	}
}
