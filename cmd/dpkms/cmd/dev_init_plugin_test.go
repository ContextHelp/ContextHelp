package cmd

import (
	"encoding/json"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/plugin"
)

// scaffoldPlugin runs `dpkms dev init-plugin <slug> --dir <tmp>` and
// returns the generated plugin directory.
func scaffoldPlugin(t *testing.T, slug string) string {
	t.Helper()
	parent := t.TempDir()
	if out, err := executeCommand("dev", "init-plugin", slug, "--dir", parent); err != nil {
		t.Fatalf("dev init-plugin: %v\n%s", err, out)
	}
	return filepath.Join(parent, slug)
}

// The scaffolded manifest must pass the same loader the runtime uses,
// otherwise every freshly generated plugin is rejected on first load.
func TestDevInitPlugin_ManifestPassesValidator(t *testing.T) {
	dir := scaffoldPlugin(t, "my-plugin")

	m, err := plugin.LoadManifest(dir)
	if err != nil {
		t.Fatalf("scaffolded manifest rejected by plugin.LoadManifest: %v", err)
	}
	if m.Name != "my-plugin" {
		t.Errorf("manifest name = %q, want my-plugin", m.Name)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.ParseManifest(raw); err != nil {
		t.Fatalf("scaffolded manifest rejected by plugin.ParseManifest: %v", err)
	}
}

// The Go stub is the other generated artifact with a real validator:
// the compiler. It must be gofmt-clean and satisfy pluginapi.Plugin.
func TestDevInitPlugin_GoStubCompiles(t *testing.T) {
	dir := scaffoldPlugin(t, "my-plugin")
	src, err := os.ReadFile(filepath.Join(dir, "plugin.go"))
	if err != nil {
		t.Fatal(err)
	}

	formatted, err := format.Source(src)
	if err != nil {
		t.Fatalf("plugin.go does not parse: %v\n%s", err, src)
	}
	if string(formatted) != string(src) {
		t.Errorf("plugin.go is not gofmt-clean:\n--- got ---\n%s\n--- gofmt ---\n%s", src, formatted)
	}

	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	gomod, err := exec.Command(goBin, "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	modRoot := filepath.Dir(strings.TrimSpace(string(gomod)))

	// Compile the stub as a package inside this module through a build
	// overlay: nothing is written to the repository tree.
	virtual := filepath.Join(modRoot, "zz_initplugin_scaffold_check", "plugin.go")
	overlay, err := json.Marshal(map[string]map[string]string{
		"Replace": {virtual: filepath.Join(dir, "plugin.go")},
	})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(t.TempDir(), "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
		t.Fatal(err)
	}

	build := exec.Command(goBin, "build", "-overlay", overlayPath, "./zz_initplugin_scaffold_check")
	build.Dir = modRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("scaffolded plugin.go does not compile: %v\n%s", err, out)
	}
}

// The README links into the repository docs; the target must exist,
// and it must not advertise config the host never reads.
func TestDevInitPlugin_ReadmeDocLinkResolves(t *testing.T) {
	dir := scaffoldPlugin(t, "my-plugin")
	readme, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	const marker = "](../../"
	i := strings.Index(string(readme), marker)
	if i < 0 {
		t.Fatalf("README has no repo-relative docs link:\n%s", readme)
	}
	rest := string(readme)[i+len(marker):]
	target := rest[:strings.IndexByte(rest, ')')]

	// Scaffold default is plugins/<slug>/, two levels below the repo root.
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, target)); err != nil {
		t.Errorf("README links to %q, which does not exist in the repo: %v", target, err)
	}

	// Plugin config blocks are not read from ctxt.yaml / dpkms.yaml; a
	// `plugins:` snippet would tell authors to write config nothing loads.
	if strings.Contains(string(readme), "plugins:") {
		t.Errorf("README carries a plugins: config snippet the config schema does not read:\n%s", readme)
	}
}
