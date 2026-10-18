package secrets

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"hop.top/kit/go/storage/secret"
)

// The env backend reads unprefixed variables (OPENAI_API_KEY), so
// listing its keys would list the name of every variable in the
// process environment. List refuses with ErrNotSupported instead; Get
// still reads a variable.
func TestEnvBackendListRefuses(t *testing.T) {
	t.Setenv("SECAUDIT_CANARY_VARIABLE", "not a ctxt secret")
	t.Setenv("OPENAI_API_KEY", "sk-from-env")
	for _, backend := range []string{"", "env"} {
		t.Run("backend="+backend, func(t *testing.T) {
			store, err := New(config.SecretsConfig{Backend: backend})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			for _, prefix := range []string{"", "SECAUDIT", "secaudit_"} {
				keys, err := store.List(ctx, prefix)
				if !errors.Is(err, secret.ErrNotSupported) {
					t.Errorf("List(%q): err = %v, want ErrNotSupported", prefix, err)
				}
				for _, k := range keys {
					if strings.Contains(strings.ToLower(k), "secaudit") {
						t.Errorf("List(%q) exposed an environment variable name: %q", prefix, k)
					}
				}
			}
			got, err := store.Get(ctx, "OPENAI_API_KEY")
			if err != nil || string(got.Value) != "sk-from-env" {
				t.Errorf("Get(OPENAI_API_KEY) = %v, %v; want the env value", got, err)
			}
		})
	}
}
