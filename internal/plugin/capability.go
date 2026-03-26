package plugin

import (
	"context"
	"io"
	"net/http"
	"os"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// CapabilityEnforcer wraps plugin API calls and returns PermissionDeniedError
// for any operation the plugin's manifest does not authorise.
type CapabilityEnforcer struct {
	pluginName string
	manifest   *pluginapi.PluginManifest
}

// NewCapabilityEnforcer creates an enforcer for the named plugin using its manifest.
// If manifest is nil, all privileged operations are denied.
func NewCapabilityEnforcer(pluginName string, manifest *pluginapi.PluginManifest) *CapabilityEnforcer {
	return &CapabilityEnforcer{pluginName: pluginName, manifest: manifest}
}

// check returns a PermissionDeniedError when the manifest does not grant perm.
func (e *CapabilityEnforcer) check(perm, operation string) error {
	if e.manifest != nil && e.manifest.HasPermission(perm) {
		return nil
	}
	return &pluginapi.PermissionDeniedError{
		Plugin:     e.pluginName,
		Permission: perm,
		Operation:  operation,
	}
}

// ─── Network ─────────────────────────────────────────────────────────────────

// FetchURL performs an HTTP GET if the plugin holds the "network" permission.
func (e *CapabilityEnforcer) FetchURL(ctx context.Context, url string) (*http.Response, error) {
	if err := e.check(pluginapi.PermNetwork, "FetchURL"); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}

// ─── Filesystem ──────────────────────────────────────────────────────────────

// ReadFile reads a file from disk if the plugin holds the "filesystem" permission.
func (e *CapabilityEnforcer) ReadFile(path string) ([]byte, error) {
	if err := e.check(pluginapi.PermFilesystem, "ReadFile"); err != nil {
		return nil, err
	}
	return os.ReadFile(path) //nolint:gosec // path provided by privileged caller
}

// WriteFile writes data to disk if the plugin holds the "filesystem" permission.
func (e *CapabilityEnforcer) WriteFile(path string, data []byte, perm os.FileMode) error {
	if err := e.check(pluginapi.PermFilesystem, "WriteFile"); err != nil {
		return err
	}
	return os.WriteFile(path, data, perm) //nolint:gosec // path provided by privileged caller
}

// ─── Clipboard ───────────────────────────────────────────────────────────────

// WatchClipboard is a no-op placeholder guarded by the "clipboard" permission.
// Real clipboard integration should inject an io.Reader here.
func (e *CapabilityEnforcer) WatchClipboard() (io.Reader, error) {
	if err := e.check(pluginapi.PermClipboard, "WatchClipboard"); err != nil {
		return nil, err
	}
	// Concrete implementation deferred to clipboard subsystem.
	return nil, nil
}

// ─── Refresh ─────────────────────────────────────────────────────────────────

// TriggerRefresh fires a refresh signal if the plugin holds the "refresh" permission.
func (e *CapabilityEnforcer) TriggerRefresh(ctx context.Context, bus pluginapi.Bus) error {
	if err := e.check(pluginapi.PermRefresh, "refresh.Trigger"); err != nil {
		return err
	}
	return bus.Publish(ctx, pluginapi.Event{Type: "ctxt.refresh.trigger"})
}

// ─── Notifications ───────────────────────────────────────────────────────────

// CreateNotification publishes a notification event if the plugin holds "notifications".
func (e *CapabilityEnforcer) CreateNotification(ctx context.Context, bus pluginapi.Bus, title, body string) error {
	if err := e.check(pluginapi.PermNotifications, "notifications.Create"); err != nil {
		return err
	}
	return bus.Publish(ctx, pluginapi.Event{Type: "ctxt.notification.create"})
}

// ─── Entity operations ───────────────────────────────────────────────────────

// CheckPublishEntity verifies the plugin may call PublishEntity / UpdateEntity.
func (e *CapabilityEnforcer) CheckPublishEntity() error {
	return e.check(pluginapi.PermEntityWrite, "PublishEntity")
}

// CheckUpdateEntity verifies the plugin may call UpdateEntity.
func (e *CapabilityEnforcer) CheckUpdateEntity() error {
	return e.check(pluginapi.PermEntityWrite, "UpdateEntity")
}

// CheckDefineAlias verifies the plugin may call DefineAlias.
func (e *CapabilityEnforcer) CheckDefineAlias() error {
	return e.check(pluginapi.PermEntityAlias, "DefineAlias")
}
