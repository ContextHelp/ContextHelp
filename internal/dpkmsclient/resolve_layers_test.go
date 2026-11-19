package dpkmsclient_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
)

// TestResolveProjectEndpointBeatsUserURLs: a project server.url wins over
// user server.urls, with no user token attached.
func TestResolveProjectEndpointBeatsUserURLs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CONFIG_DIRS", "")
	t.Setenv(config.EnvConfigPath, "")
	t.Setenv(config.EnvDataDir, filepath.Join(home, "data"))
	udir := filepath.Join(home, ".config", "contexthelp")
	proj := filepath.Join(home, "work", "proj")
	pdir := filepath.Join(proj, ".contexthelp")
	for _, d := range []string{udir, pdir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(udir, "ctxt.yaml"), "server:\n  token: kUser\n  urls:\n    - {name: pers, url: 'http://a.example.net:1', token: kA}\n")
	write(filepath.Join(pdir, "ctxt.yaml"), "server:\n  url: http://b.example.net:2\n")
	prev, _ := os.Getwd()
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	cfg, err := config.Load("ctxt", "")
	if err != nil {
		t.Fatal(err)
	}
	r := resolve(t, cfg.Server, dpkmsclient.Selection{}, noLocals(t))
	if r.URL != "http://b.example.net:2" || r.Layer != dpkmsclient.LayerURL || r.Token != "" || r.Name != "" {
		t.Fatalf("got %+v, want project server.url, layer server.url, no token", r)
	}
}
