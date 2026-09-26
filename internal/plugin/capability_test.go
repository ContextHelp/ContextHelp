package plugin_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/plugin"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

func manifestWith(perms ...string) *pluginapi.PluginManifest {
	return &pluginapi.PluginManifest{
		APIVersion:  "v1",
		Name:        "test-plugin",
		Version:     "1.0.0",
		Permissions: perms,
	}
}

func TestCapabilityEnforcer_ReadFile_denied(t *testing.T) {
	e := plugin.NewCapabilityEnforcer("test-plugin", manifestWith())
	_, err := e.ReadFile("/tmp/foo")
	require.Error(t, err)
	var pde *pluginapi.PermissionDeniedError
	require.ErrorAs(t, err, &pde)
	require.Equal(t, pluginapi.PermFilesystem, pde.Permission)
}

func TestCapabilityEnforcer_ReadFile_allowed(t *testing.T) {
	// Create a temp file so ReadFile can actually succeed.
	dir := t.TempDir()
	p := dir + "/test.txt"
	require.NoError(t, os.WriteFile(p, []byte("hello"), 0o600))

	e := plugin.NewCapabilityEnforcer("test-plugin", manifestWith(pluginapi.PermFilesystem))
	data, err := e.ReadFile(p)
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), data)
}

func TestCapabilityEnforcer_WriteFile_denied(t *testing.T) {
	e := plugin.NewCapabilityEnforcer("test-plugin", manifestWith())
	err := e.WriteFile("/tmp/x", []byte("x"), 0o600)
	require.Error(t, err)
	var pde *pluginapi.PermissionDeniedError
	require.ErrorAs(t, err, &pde)
	require.Equal(t, pluginapi.PermFilesystem, pde.Permission)
}

func TestCapabilityEnforcer_WatchClipboard_denied(t *testing.T) {
	e := plugin.NewCapabilityEnforcer("test-plugin", manifestWith())
	_, err := e.WatchClipboard()
	require.Error(t, err)
	var pde *pluginapi.PermissionDeniedError
	require.ErrorAs(t, err, &pde)
	require.Equal(t, pluginapi.PermClipboard, pde.Permission)
}

func TestCapabilityEnforcer_TriggerRefresh_denied(t *testing.T) {
	e := plugin.NewCapabilityEnforcer("test-plugin", manifestWith())
	err := e.TriggerRefresh(context.Background(), &stubBus{})
	require.Error(t, err)
	var pde *pluginapi.PermissionDeniedError
	require.ErrorAs(t, err, &pde)
	require.Equal(t, pluginapi.PermRefresh, pde.Permission)
}

func TestCapabilityEnforcer_CreateNotification_denied(t *testing.T) {
	e := plugin.NewCapabilityEnforcer("test-plugin", manifestWith())
	err := e.CreateNotification(context.Background(), &stubBus{}, "title", "body")
	require.Error(t, err)
	var pde *pluginapi.PermissionDeniedError
	require.ErrorAs(t, err, &pde)
	require.Equal(t, pluginapi.PermNotifications, pde.Permission)
}

func TestCapabilityEnforcer_EntityWrite_denied(t *testing.T) {
	e := plugin.NewCapabilityEnforcer("test-plugin", manifestWith())
	require.Error(t, e.CheckPublishEntity())
	require.Error(t, e.CheckUpdateEntity())
}

func TestCapabilityEnforcer_EntityAlias_denied(t *testing.T) {
	e := plugin.NewCapabilityEnforcer("test-plugin", manifestWith())
	require.Error(t, e.CheckDefineAlias())
}

func TestCapabilityEnforcer_EntityWrite_allowed(t *testing.T) {
	e := plugin.NewCapabilityEnforcer("test-plugin", manifestWith(pluginapi.PermEntityWrite))
	require.NoError(t, e.CheckPublishEntity())
	require.NoError(t, e.CheckUpdateEntity())
}

func TestCapabilityEnforcer_NilManifest_denied(t *testing.T) {
	e := plugin.NewCapabilityEnforcer("test-plugin", nil)
	_, err := e.ReadFile("/tmp/x")
	require.Error(t, err)
}

// stubBus satisfies pluginapi.Bus for tests.
type stubBus struct{}

func (s *stubBus) Publish(_ context.Context, _ pluginapi.Event) error { return nil }
func (s *stubBus) Subscribe(_ string, _ func(context.Context, pluginapi.Event) error) {
}
func (s *stubBus) Close() error { return nil }

// recordingPluginBus keeps the type of every published event.
type recordingPluginBus struct{ types []string }

func (b *recordingPluginBus) Publish(_ context.Context, e pluginapi.Event) error {
	b.types = append(b.types, e.Type)
	return nil
}

func (b *recordingPluginBus) Subscribe(_ string, _ func(context.Context, pluginapi.Event) error) {
}
func (b *recordingPluginBus) Close() error { return nil }

func TestCapabilityEnforcer_PublishesKitTopics(t *testing.T) {
	e := plugin.NewCapabilityEnforcer("test-plugin", manifestWith(pluginapi.PermRefresh, pluginapi.PermNotifications))
	b := &recordingPluginBus{}
	require.NoError(t, e.TriggerRefresh(context.Background(), b))
	require.NoError(t, e.CreateNotification(context.Background(), b, "title", "body"))
	require.Equal(t, []string{
		string(events.TopicPluginRefreshRequested),
		string(events.TopicPluginNotificationRequested),
	}, b.types)
	for _, typ := range b.types {
		require.NoError(t, bus.ValidateTopic(bus.Topic(typ)))
	}
}
