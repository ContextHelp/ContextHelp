package steps_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// skill is a SKILL.md for a step named name running ./run.sh, with
// extra lines appended to its execution block.
func skill(name string, extra ...string) string {
	lines := []string{
		"---",
		"name: " + name,
		"version: 1.0.0",
		"execution:",
		"  type: external",
		"  executable: run.sh",
	}
	for _, e := range extra {
		lines = append(lines, "  "+e)
	}
	return strings.Join(append(lines, "---", "", "test step"), "\n")
}

// stepDir writes a step directory: SKILL.md and an executable run.sh
// holding script.
func stepDir(t *testing.T, skillMD, script string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil { // #nosec G306 -- test step executable
		t.Fatal(err)
	}
	return dir
}

type fixture struct {
	store     storage.StorageDriver
	installer *steps.Installer
	stepsDir  string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	store := storageutil.NewTestDriver(t)
	dir := filepath.Join(t.TempDir(), "steps")
	return &fixture{
		store:     store,
		stepsDir:  dir,
		installer: &steps.Installer{Dir: dir, Store: store, Discovery: steps.NewStepDiscovery(store)},
	}
}

// install packs and installs a step directory from the local path.
func (f *fixture) install(t *testing.T, name, skillMD, script string) *storage.RegisteredStep {
	t.Helper()
	archive, err := steps.PackDir(stepDir(t, skillMD, script))
	if err != nil {
		t.Fatal(err)
	}
	rs, err := f.installer.InstallArchive(context.Background(), name, archive, "local", "")
	if err != nil {
		t.Fatalf("install %s: %v", name, err)
	}
	return rs
}

// step builds the installed step for pipeline "text.short" with the
// process runner.
func (f *fixture) step(t *testing.T, ins config.StepInsert) *steps.ExternalStep {
	t.Helper()
	cat, err := steps.LoadCatalog(context.Background(), f.store, steps.DefaultRunners())
	if err != nil {
		t.Fatal(err)
	}
	cat.WorkRoot = t.TempDir()
	s, err := cat.Step("text.short", ins)
	if err != nil {
		t.Fatalf("build step: %v", err)
	}
	return s.(*steps.ExternalStep)
}

func draft() *storage.KnowledgeObject {
	return &storage.KnowledgeObject{
		ID:         "ko-1",
		RawContent: "mail jane@example.com",
		Source:     "cli",
		Pipeline:   "text.short",
		CreatedAt:  time.Date(2026, 10, 14, 9, 30, 0, 123, time.UTC),
		Vectors:    []storage.ObjectVector{{ModelID: "m"}},
	}
}

// emit is a step script that drains stdin and prints out.
func emit(out string) string {
	return "cat >/dev/null\nprintf '%s' '" + out + "'"
}
