package plugin_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/plugin"
	"github.com/stretchr/testify/require"
)

func TestRedactSecrets_anthropicKey(t *testing.T) {
	input := "The key is sk-ant-api03-ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	out := plugin.RedactSecrets(input)
	require.Contains(t, out, "[REDACTED]")
	require.NotContains(t, out, "sk-ant-api03")
}

func TestRedactSecrets_openAIKey(t *testing.T) {
	input := "key=sk-ABCDEFGHIJKLMNOPQRSTUVWXYZ12345"
	out := plugin.RedactSecrets(input)
	require.Contains(t, out, "[REDACTED]")
}

func TestRedactSecrets_githubToken(t *testing.T) {
	input := "ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	out := plugin.RedactSecrets(input)
	require.Contains(t, out, "[REDACTED]")
}

func TestRedactSecrets_jwt(t *testing.T) {
	input := "token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ"
	out := plugin.RedactSecrets(input)
	require.Contains(t, out, "[REDACTED]")
}

func TestRedactSecrets_noSecret(t *testing.T) {
	input := "hello world, nothing secret here"
	out := plugin.RedactSecrets(input)
	require.Equal(t, input, out)
}

func TestRedactSecrets_passwordField(t *testing.T) {
	input := "password: supersecretpassword123"
	out := plugin.RedactSecrets(input)
	require.Contains(t, out, "[REDACTED]")
}

func TestRedactMap(t *testing.T) {
	m := map[string]any{
		"msg":   "key=sk-ABCDEFGHIJKLMNOPQRSTUVWXYZ12345",
		"count": 42,
		"nested": map[string]any{
			"token": "ghp_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		},
	}
	out := plugin.RedactMap(m)
	require.Contains(t, out["msg"], "[REDACTED]")
	require.Equal(t, 42, out["count"])
	nested := out["nested"].(map[string]any)
	require.Contains(t, nested["token"], "[REDACTED]")
}
