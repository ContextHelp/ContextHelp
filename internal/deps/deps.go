// Package deps provides a registry of installable runtime dependencies.
// Pipelines, detectors, and plugins register their requirements here so that
// 'dpkms install-deps' can install everything in one shot.
package deps

import "sync"

// Dep describes a single installable dependency.
type Dep struct {
	// Binary is the tool name to probe for (via PATH and common local dirs).
	Binary string
	// BrewPkg, AptPkg, DnfPkg, PacmanPkg are package names for each system manager.
	BrewPkg   string
	AptPkg    string
	DnfPkg    string
	PacmanPkg string
	// PipPkg is the PyPI package name. Empty means not a Python package.
	PipPkg string
	// Description is shown in install-deps output.
	Description string
}

var (
	mu       sync.Mutex
	registry []Dep
)

// Register adds one or more Deps to the global registry.
// Safe to call from multiple init() functions concurrently.
func Register(ds ...Dep) {
	mu.Lock()
	defer mu.Unlock()
	registry = append(registry, ds...)
}

// All returns a snapshot of the registered deps in registration order.
func All() []Dep {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Dep, len(registry))
	copy(out, registry)
	return out
}
