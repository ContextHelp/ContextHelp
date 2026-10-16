package http

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// templatesAsRows returns the built-in policy templates as store rows.
func templatesAsRows(t *testing.T) []storage.SharePolicy {
	t.Helper()
	var out []storage.SharePolicy
	for _, tpl := range sharing.Templates() {
		spec, err := tpl.Spec()
		require.NoError(t, err)
		out = append(out, storage.SharePolicy{Name: tpl.Name, Spec: spec})
	}
	return out
}
