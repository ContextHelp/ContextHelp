package aliasing_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	aliasing "github.com/ideacrafterslabs/ctxt-plugin-aliasing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runCLI runs the alias command tree against baseURL and returns stdout.
func runCLI(t *testing.T, baseURL string, args ...string) (string, error) {
	t.Helper()
	cmd := aliasing.NewCLICommands(baseURL)
	cmd.SilenceUsage = true // usage text is the mounting root's concern
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// server answers every request with status and body.
func server(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

const conflictBody = `{"error":{"code":"CONFLICT","message":"alias \"k8s\" already exists","details":{}}}`

// A rejected request fails the command with the server's error and
// prints no success line.
func TestCLI_NonSuccessStatusFails(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		args   []string
		want   string
	}{
		"set conflict": {http.StatusConflict, conflictBody, []string{"set", "k8s", "obj-1"},
			`409 CONFLICT: alias "k8s" already exists`},
		"set unknown object": {http.StatusNotFound, `{"error":{"code":"NOT_FOUND","message":"object not found"}}`,
			[]string{"set", "k8s", "nope"}, "404 NOT_FOUND: object not found"},
		"set non-JSON body": {http.StatusBadGateway, "upstream down", []string{"set", "k8s", "obj-1"},
			"502 Bad Gateway: upstream down"},
		"remove missing": {http.StatusNotFound, `{"error":{"code":"NOT_FOUND","message":"alias not found"}}`,
			[]string{"remove", "k8s"}, "404 NOT_FOUND: alias not found"},
		"list unsupported": {http.StatusNotImplemented, `{"error":{"code":"NOT_IMPLEMENTED","message":"aliases are not supported by this storage backend"}}`,
			[]string{"list"}, "501 NOT_IMPLEMENTED: aliases are not supported by this storage backend"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := runCLI(t, server(t, tc.status, tc.body).URL, tc.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Empty(t, out)
		})
	}
}

func TestCLI_SuccessPrints(t *testing.T) {
	out, err := runCLI(t, server(t, http.StatusCreated, `{"alias":"k8s","object_id":"obj-1"}`).URL, "set", "k8s", "obj-1")
	require.NoError(t, err)
	assert.Equal(t, "alias \"k8s\" → obj-1\n", out)

	out, err = runCLI(t, server(t, http.StatusNoContent, "").URL, "remove", "k8s")
	require.NoError(t, err)
	assert.Equal(t, "removed alias \"k8s\"\n", out)

	out, err = runCLI(t, server(t, http.StatusOK, `[{"alias":"k8s","object_id":"obj-1","scope":"global"}]`).URL, "list")
	require.NoError(t, err)
	assert.Equal(t, "k8s → obj-1 (scope=global)\n", out)
}
