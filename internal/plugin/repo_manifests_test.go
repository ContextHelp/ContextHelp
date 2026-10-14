package plugin_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/plugin"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
)

// repoRoot walks up from the package directory to the directory that holds
// go.mod and plugins/.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		_, modErr := os.Stat(filepath.Join(dir, "go.mod"))
		_, plugErr := os.Stat(filepath.Join(dir, "plugins"))
		if modErr == nil && plugErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "repo root (go.mod + plugins/) not found")
		dir = parent
	}
}

// manifestFiles returns every manifest.json / manifest.yaml / manifest.yml
// under root/sub, skipping node_modules.
func manifestFiles(t *testing.T, root, sub string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(filepath.Join(root, sub), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "node_modules" {
			return filepath.SkipDir
		}
		switch d.Name() {
		case "manifest.json", "manifest.yaml", "manifest.yml":
			out = append(out, p)
		}
		return nil
	})
	require.NoError(t, err)
	return out
}

// Every plugin manifest in the repo must load through plugin.LoadManifest
// (pluginapi v1), and a plugin directory carries exactly one: LoadManifest
// reads manifest.json first, so a second file would be shadowed.
func TestRepoPluginManifestsLoad(t *testing.T) {
	root := repoRoot(t)
	files := manifestFiles(t, root, "plugins")
	require.NotEmpty(t, files, "no plugin manifests found under plugins/")

	byDir := map[string][]string{}
	for _, f := range files {
		dir := filepath.Dir(f)
		require.Equal(t, filepath.Join(root, "plugins"), filepath.Dir(dir),
			"plugin manifest must sit in plugins/<name>/: %s", f)
		require.NotEqual(t, "manifest.yml", filepath.Base(f),
			"LoadManifest does not read manifest.yml: %s", f)
		byDir[dir] = append(byDir[dir], filepath.Base(f))
	}
	for dir, names := range byDir {
		rel, _ := filepath.Rel(root, dir)
		t.Run(rel, func(t *testing.T) {
			require.Len(t, names, 1, "%s has %v; keep one manifest per plugin", rel, names)
			_, err := plugin.LoadManifest(dir)
			require.NoError(t, err, "%s/%s", rel, names[0])
		})
	}
}

// Extensions and skills are not Go plugins: their manifest.json is the
// contribution manifest described by .github/manifest.schema.json.
func TestRepoContributionManifestsMatchSchema(t *testing.T) {
	root := repoRoot(t)

	schemaPath := filepath.Join(root, ".github", "manifest.schema.json")
	f, err := os.Open(schemaPath) // #nosec G304 -- repo file
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	schemaDoc, err := jsonschema.UnmarshalJSON(f)
	require.NoError(t, err)
	c := jsonschema.NewCompiler()
	require.NoError(t, c.AddResource("manifest.schema.json", schemaDoc))
	schema, err := c.Compile("manifest.schema.json")
	require.NoError(t, err)

	var files []string
	for _, sub := range []string{"extensions", "skills"} {
		files = append(files, manifestFiles(t, root, sub)...)
	}
	require.NotEmpty(t, files, "no contribution manifests found")
	for _, p := range files {
		rel, _ := filepath.Rel(root, p)
		t.Run(rel, func(t *testing.T) {
			require.Equal(t, "manifest.json", filepath.Base(p), "contribution manifests are manifest.json")
			raw, err := os.Open(p) // #nosec G304 -- repo file
			require.NoError(t, err)
			defer func() { _ = raw.Close() }()
			inst, err := jsonschema.UnmarshalJSON(raw)
			require.NoError(t, err)
			require.NoError(t, schema.Validate(inst))
		})
	}
}
