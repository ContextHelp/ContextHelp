package registry

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

type stubSource map[string]string

func (s stubSource) Get(name string) (string, error) {
	if tok, ok := s[name]; ok {
		return tok, nil
	}
	return "", ErrNoToken{RegistryName: name}
}

func TestLazyTokens_OpensOnFirstLookupOnly(t *testing.T) {
	opened := 0
	src := newLazyTokens(func() TokenSource {
		opened++
		return stubSource{"paid": "fake-registry-token"}
	})

	// Building credentials, even for an authenticated registry, opens nothing.
	_ = NewCredentials([]config.RegistryConfig{{
		Name: "paid", URL: "https://paid.example",
		Auth: config.RegistryAuthConfig{Type: config.RegistryAuthBearer},
	}}, src)
	if opened != 0 {
		t.Fatalf("store opened %d times before any lookup", opened)
	}

	for range 2 {
		if tok, err := src.Get("paid"); err != nil || tok != "fake-registry-token" {
			t.Fatalf("Get = %q, %v", tok, err)
		}
	}
	if opened != 1 {
		t.Errorf("store opened %d times, want 1", opened)
	}
}
