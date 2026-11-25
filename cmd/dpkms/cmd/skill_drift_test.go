package cmd

import (
	"path/filepath"
	"testing"

	"hop.top/sdg"
)

func TestSkillDrift(t *testing.T) {
	prepareTree()
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
			Name:           "dpkms",
			Router:         "references/dpkms.md",
			RefsDir:        "references/dpkms",
			Tree:           sdg.FromCobra(rootCmd),
			IgnoreCommands: []string{"help", "completion", "spec"},
		}},
	})
}
