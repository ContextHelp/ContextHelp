package cmd

import (
	"strings"
	"testing"
)

// dpkms dev reindex-vectors is removed: `ctxt embeddings migrate` backfills
// vectors per registered model. The remaining dev group stays
// signature-clean.
func TestDevReindexVectorsRemoved(t *testing.T) {
	for _, c := range devCmd.Commands() {
		if c.Name() == "reindex-vectors" {
			t.Fatal("dpkms dev reindex-vectors still registered")
		}
	}
	resetAllFlags(rootCmd)
	for _, v := range root.ValidateSignature().Violations {
		if strings.Contains(v.Path, " dev") {
			t.Errorf("signature violation: %s [%s/%s] %s", v.Path, v.Check, v.Severity, v.Detail)
		}
	}
}
