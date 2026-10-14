package scripts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// catalogDirs are the contribution trees whose entries carry a catalog.json.
var catalogDirs = []string{"extensions", "skills"}

// contributionDirs returns every <tree>/<slug> directory under the repo root.
func contributionDirs(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, tree := range catalogDirs {
		entries, err := os.ReadDir(filepath.Join("..", tree))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() {
				out = append(out, filepath.Join("..", tree, e.Name()))
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("no contributions found under extensions/ or skills/")
	}
	return out
}

// TestCatalogEntriesMatchSchema validates every extensions/<slug>/catalog.json
// and skills/<slug>/catalog.json against .github/catalog.schema.json.
func TestCatalogEntriesMatchSchema(t *testing.T) {
	f, err := os.Open(filepath.Join("..", ".github", "catalog.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	schemaDoc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("catalog.schema.json", schemaDoc); err != nil {
		t.Fatal(err)
	}
	schema, err := c.Compile("catalog.schema.json")
	if err != nil {
		t.Fatal(err)
	}

	for _, dir := range contributionDirs(t) {
		p := filepath.Join(dir, "catalog.json")
		rel, _ := filepath.Rel("..", p)
		t.Run(filepath.ToSlash(rel), func(t *testing.T) {
			raw, err := os.Open(p) // #nosec G304 -- repo file
			if err != nil {
				t.Fatalf("every contribution needs a catalog.json: %v", err)
			}
			defer func() { _ = raw.Close() }()
			inst, err := jsonschema.UnmarshalJSON(raw)
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(inst); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestNoContributionManifestJSON fails if any file named manifest.json is
// left under extensions/ or skills/: the contribution format is catalog.json.
func TestNoContributionManifestJSON(t *testing.T) {
	for _, tree := range catalogDirs {
		err := filepath.WalkDir(filepath.Join("..", tree), func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			if d.Name() == "manifest.json" {
				t.Errorf("%s: rename to catalog.json (docs/catalog-spec.md)", p)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
