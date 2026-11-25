package cmd

import (
	"path/filepath"
	"testing"

	"hop.top/sdg"
)

func TestSkillDrift(t *testing.T) {
	if err := prepareTree(); err != nil {
		t.Fatalf("prepareTree: %v", err)
	}
	skillRoot := filepath.Join("..", "..", "..", "skills", "ctxt")
	sdg.Verify(t, sdg.Config{
		SkillRoot:  skillRoot,
		Entrypoint: "SKILL.md",
		Manifest:   &sdg.ManifestConfig{Path: "catalog.json"},
		Budgets: sdg.BudgetsConfig{
			EntrypointMaxLines: 150,
			RouterMaxLines:     250,
			ReferenceMaxLines:  500,
		},
		Binaries: []sdg.BinaryConfig{{
			Name:           "ctxt",
			Router:         "references/ctxt.md",
			RefsDir:        "references/ctxt",
			Tree:           sdg.FromCobra(rootCmd),
			IgnoreCommands: []string{"help", "completion", "spec"},
		}},
	})
}
