package cmd

import (
	"strings"
	"testing"
)

// `dpkms secret list` on the env backend (the default) never prints the
// process environment: the backend reads unprefixed variables, so it
// says keys cannot be enumerated instead of listing every variable name.
func TestSecretListEnvBackendOmitsEnvironment(t *testing.T) {
	t.Setenv("SECAUDIT_CANARY_VARIABLE", "not a ctxt secret")
	for _, args := range [][]string{
		{"secret", "list"},
		{"secret", "list", "--format", "json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, err := executeCommand(args...)
			if err != nil {
				t.Fatalf("%v: %v\n%s", args, err, out)
			}
			if strings.Contains(strings.ToLower(out), "secaudit") {
				t.Errorf("%v listed an environment variable name:\n%s", args, out)
			}
			if !strings.Contains(out, "env") {
				t.Errorf("%v does not name the env backend:\n%s", args, out)
			}
		})
	}
}
