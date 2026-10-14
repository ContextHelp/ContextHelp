package steps_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// fakeToken is a fixed, non-secret value; failure messages may print it.
const fakeToken = "fake-registry-token"

type mapTokens map[string]string

func (m mapTokens) Get(name string) (string, error) {
	if tok, ok := m[name]; ok {
		return tok, nil
	}
	return "", registry.ErrNoToken{RegistryName: name}
}

// stepRegistry serves a manifest with one step and that step's SKILL.md,
// both only to requests carrying the bearer token.
func stepRegistry(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+fakeToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/", "":
			_ = json.NewEncoder(w).Encode(storage.RegistryManifest{
				Name:    "paid",
				Version: "1.0.0",
				Steps:   []storage.ManifestStep{{Name: "paid_step", Version: "1.0.0", Path: "steps/paid_step/SKILL.md"}},
			})
		case "/steps/paid_step/SKILL.md":
			_, _ = w.Write([]byte("---\nname: paid_step\nversion: 1.0.0\n---\nbody\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newDiscovery(t *testing.T, srvURL string, tokens registry.TokenSource) *steps.StepDiscovery {
	t.Helper()
	regs := []config.RegistryConfig{{
		Name: "paid", URL: srvURL,
		Auth: config.RegistryAuthConfig{Type: config.RegistryAuthBearer},
	}}
	return steps.NewStepDiscovery(storageutil.NewTestDriver(t), t.TempDir()).
		WithCredentials(registry.NewCredentials(regs, tokens))
}

func TestDiscovery_ManifestAndStepDownloadCarryToken(t *testing.T) {
	srv := stepRegistry(t)
	sd := newDiscovery(t, srv.URL, mapTokens{"paid": fakeToken})
	ctx := context.Background()

	if _, err := sd.FetchRemoteManifest(ctx, srv.URL); err != nil {
		t.Fatalf("FetchRemoteManifest: %v", err)
	}
	if _, err := sd.FetchRegistryManifest(ctx, srv.URL); err != nil {
		t.Fatalf("FetchRegistryManifest: %v", err)
	}
	if err := sd.InstallStep(ctx, "paid_step", srv.URL); err != nil {
		t.Fatalf("InstallStep: %v", err)
	}
}

func TestDiscovery_NoToken_LoginHint(t *testing.T) {
	srv := stepRegistry(t)
	sd := newDiscovery(t, srv.URL, mapTokens{})
	ctx := context.Background()

	_, err := sd.FetchRemoteManifest(ctx, srv.URL)
	if err == nil || !strings.Contains(err.Error(), "ctxt registry login paid") {
		t.Errorf("FetchRemoteManifest without token: err = %v", err)
	}
	err = sd.InstallStep(ctx, "paid_step", srv.URL)
	if err == nil || !strings.Contains(err.Error(), "ctxt registry login paid") {
		t.Errorf("InstallStep without token: err = %v", err)
	}
}
