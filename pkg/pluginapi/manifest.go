// Package pluginapi — manifest types shared by all plugins.
package pluginapi

// Permission constants for plugin manifests.
const (
	PermReadObjects   = "read_objects"
	PermWriteObjects  = "write_objects"
	PermCallLLM       = "call_llm"
	PermNetwork       = "network"
	PermFilesystem    = "filesystem"
	PermClipboard     = "clipboard"
	PermRefresh       = "refresh"
	PermNotifications = "notifications"
	PermEntityWrite   = "entity.write"
	PermEntityAlias   = "entity.alias"
	PermRegistryRead  = "registry.read"
)

// PluginType classifies the primary role of a plugin.
// A plugin manifest may omit this field; it is treated as a general plugin.
// Recognised values: registry_provider.
type PluginType string

const (
	// PluginTypeRegistryProvider marks a plugin that implements PluginRegistryProvider.
	PluginTypeRegistryProvider PluginType = "registry_provider"
)

// PrivilegedPermissions are permissions requiring explicit user confirmation
// on first use or when changed.
var PrivilegedPermissions = []string{PermEntityWrite, PermEntityAlias}

// PluginManifest declares identity, version, and permissions for a plugin.
// Loaded from manifest.json or manifest.yaml in the plugin's directory.
type PluginManifest struct {
	// APIVersion is the manifest schema version (currently "v1").
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	// Name must match the plugin's Name() return value.
	Name string `yaml:"name" json:"name"`
	// Slug is the unique identifier for the plugin.
	Slug string `yaml:"slug" json:"slug"`
	// Version is the semver string for this plugin.
	Version string `yaml:"version" json:"version"`
	// Description is a human-readable summary.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// Type classifies the plugin's primary role. Optional; omit for general plugins.
	// Currently supported: "registry_provider".
	Type PluginType `yaml:"type,omitempty" json:"type,omitempty"`
	// Permissions lists all capabilities the plugin requires.
	// Valid values: read_objects, write_objects, call_llm, network,
	// filesystem, clipboard, refresh, notifications, entity.write, entity.alias,
	// registry.read.
	Permissions []string `yaml:"permissions" json:"permissions"`
}

// HasPermission reports whether m grants perm.
func (m *PluginManifest) HasPermission(perm string) bool {
	for _, p := range m.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}

// Validate returns an error if the manifest is structurally invalid.
func (m *PluginManifest) Validate() error {
	if m.APIVersion == "" {
		return &ManifestError{Name: m.Name, Reason: "apiVersion is required"}
	}
	if m.APIVersion != "v1" {
		return &ManifestError{Name: m.Name, Reason: "unsupported apiVersion: " + m.APIVersion}
	}
	if m.Name == "" {
		return &ManifestError{Name: "(unknown)", Reason: "name is required"}
	}
	if m.Version == "" {
		return &ManifestError{Name: m.Name, Reason: "version is required"}
	}
	for _, p := range m.Permissions {
		if !isKnownPermission(p) {
			return &ManifestError{Name: m.Name, Reason: "unknown permission: " + p}
		}
	}
	return nil
}

// ManifestError is returned when a manifest fails validation.
type ManifestError struct {
	Name   string
	Reason string
}

func (e *ManifestError) Error() string {
	return "plugin manifest " + e.Name + ": " + e.Reason
}

// PermissionDeniedError is returned when a plugin calls an API it lacks permission for.
type PermissionDeniedError struct {
	Plugin     string
	Permission string
	Operation  string
}

func (e *PermissionDeniedError) Error() string {
	return "plugin " + e.Plugin + ": permission denied: " + e.Operation +
		" requires " + e.Permission
}

// allKnownPermissions enumerates every valid permission token.
var allKnownPermissions = []string{
	PermReadObjects, PermWriteObjects, PermCallLLM,
	PermNetwork, PermFilesystem, PermClipboard,
	PermRefresh, PermNotifications,
	PermEntityWrite, PermEntityAlias,
	PermRegistryRead,
}

func isKnownPermission(p string) bool {
	for _, k := range allKnownPermissions {
		if k == p {
			return true
		}
	}
	return false
}
