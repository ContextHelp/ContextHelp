package plugin_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/plugin"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/stretchr/testify/require"
)

func TestParseManifest_valid(t *testing.T) {
	yaml := []byte(`
apiVersion: v1
name: example
version: 1.2.3
description: an example plugin
permissions:
  - read_objects
  - call_llm
`)
	m, err := plugin.ParseManifest(yaml)
	require.NoError(t, err)
	require.Equal(t, "example", m.Name)
	require.Equal(t, "v1", m.APIVersion)
	require.Equal(t, "1.2.3", m.Version)
	require.True(t, m.HasPermission(pluginapi.PermReadObjects))
	require.True(t, m.HasPermission(pluginapi.PermCallLLM))
	require.False(t, m.HasPermission(pluginapi.PermNetwork))
}

func TestParseManifest_missingName(t *testing.T) {
	yaml := []byte(`apiVersion: v1
version: 1.0.0
permissions: []
`)
	_, err := plugin.ParseManifest(yaml)
	require.Error(t, err)
	require.Contains(t, err.Error(), "name is required")
}

func TestParseManifest_unknownPermission(t *testing.T) {
	yaml := []byte(`
apiVersion: v1
name: bad-plugin
version: 1.0.0
permissions:
  - super_admin
`)
	_, err := plugin.ParseManifest(yaml)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown permission")
}

func TestParseManifest_unsupportedAPIVersion(t *testing.T) {
	yaml := []byte(`
apiVersion: v2
name: future
version: 1.0.0
permissions: []
`)
	_, err := plugin.ParseManifest(yaml)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported apiVersion")
}

func TestCheckPermissionChange(t *testing.T) {
	old := &pluginapi.PluginManifest{Permissions: []string{"read_objects"}}
	updated := &pluginapi.PluginManifest{Permissions: []string{"read_objects", "network"}}

	added := plugin.CheckPermissionChange(old, updated)
	require.Equal(t, []string{"network"}, added)
}

func TestCheckPermissionChange_noChange(t *testing.T) {
	old := &pluginapi.PluginManifest{Permissions: []string{"read_objects"}}
	updated := &pluginapi.PluginManifest{Permissions: []string{"read_objects"}}

	added := plugin.CheckPermissionChange(old, updated)
	require.Empty(t, added)
}

func TestCheckPermissionChange_nilOld(t *testing.T) {
	updated := &pluginapi.PluginManifest{Permissions: []string{"read_objects", "network"}}
	added := plugin.CheckPermissionChange(nil, updated)
	require.Equal(t, []string{"read_objects", "network"}, added)
}
