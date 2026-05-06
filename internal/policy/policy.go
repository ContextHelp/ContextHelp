// Package policy wires kit/runtime/policy into ctxt's daemon.
//
// Init builds a fresh CEL-backed engine each call, loading YAML from
// $XDG_CONFIG_HOME/contexthelp/policy/ctxt.yaml (overridable via
// $CTXT_POLICY_FILE), and subscribes the engine to the kit
// pre_persisted topic on the supplied bus. Lifecycle is caller-managed:
// the daemon constructs one Bootstrap at serve startup and Close()s it
// on shutdown. There is no package-level cache, so calling Init twice
// against the same bus would double-subscribe — keep the daemon's
// single-Init invariant intact.
//
// Phase 2 (2026-05-06 ADR-065 amendment) relocates the YAML from the
// flat $XDG_CONFIG_HOME/contexthelp/policies.yaml that PR #23 used to
// the namespaced policy/ctxt.yaml under a new policy/ subdirectory
// alongside policy/ambient.yaml. The migration is automatic: on first
// Init after upgrade, if only the legacy path exists, the file moves
// to the new location. Operators with custom $CTXT_POLICY_FILE
// overrides must update them manually — env-overridden paths are
// never migrated.
//
// Bootstrap from cmd/dpkms/cmd/serve.go, BEFORE service.New so the
// EventPublisher returned here can be plumbed into pipeline ops:
//
//	pol, err := policy.Init(hubBus)
//	if err != nil { return err }
//	defer pol.Close()
//	svc := service.New(..., service.WithPolicyPublisher(pol.Publisher()))
package policy

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"hop.top/kit/go/core/xdg"
	"hop.top/kit/go/runtime/bus"
	"hop.top/kit/go/runtime/policy"
	"hop.top/kit/go/runtime/policy/withcel"
)

// EnvPolicyFile overrides the policies.yaml path. Tests + CI use it.
const EnvPolicyFile = "CTXT_POLICY_FILE"

// xdgTool matches internal/config so all ctxt config sits under one
// $XDG_CONFIG_HOME directory.
const xdgTool = "contexthelp"

//go:embed policies_default.yaml
var defaultPoliciesYAML []byte

// Bootstrap holds the engine + bus subscription so the daemon can
// tear them down on shutdown without leaking goroutines.
type Bootstrap struct {
	Engine    *policy.Engine
	publisher *busPublisher
	unwire    func()
	once      sync.Once
}

// Init loads the policy YAML, builds a CEL-backed engine, wires it to
// the supplied bus, and returns a Bootstrap whose Publisher() can be
// passed to domain.Service[T] via domain.WithPublisher. Misconfig
// (bad YAML, unknown topic, broken CEL) fails loud here so the daemon
// never serves traffic against an unenforced ruleset.
//
// Resolution order for the YAML source:
//
//  1. $CTXT_POLICY_FILE if set.
//  2. $XDG_CONFIG_HOME/contexthelp/policy/ctxt.yaml. On first boot
//     after the Phase 2 upgrade, if only the legacy
//     $XDG_CONFIG_HOME/contexthelp/policies.yaml path exists, the
//     file is auto-relocated. The new path is then seeded from the
//     bundled default if missing/empty (operator-authored content
//     never clobbered).
func Init(b bus.Bus) (*Bootstrap, error) {
	if b == nil {
		return nil, fmt.Errorf("policy: bus is required")
	}
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	eng, err := withcel.New(cfg, policy.WithPrincipalResolver(ctxtPrincipalResolver))
	if err != nil {
		return nil, fmt.Errorf("policy: build engine: %w", err)
	}
	bp := &Bootstrap{
		Engine:    eng,
		publisher: newBusPublisher(b),
		unwire:    policy.Wire(b, eng),
	}
	return bp, nil
}

// Publisher returns a domain.EventPublisher that forwards
// kit.runtime.entity.* events to the wired bus. Nil-safe for callers
// that received Init failure.
func (b *Bootstrap) Publisher() *busPublisher {
	if b == nil {
		return nil
	}
	return b.publisher
}

// Close unsubscribes the engine from the bus. Idempotent.
func (b *Bootstrap) Close() {
	if b == nil {
		return
	}
	b.once.Do(func() {
		if b.unwire != nil {
			b.unwire()
			b.unwire = nil
		}
	})
}

// loadConfig resolves the policy YAML and returns the parsed Config.
func loadConfig() (*policy.Config, error) {
	if path := os.Getenv(EnvPolicyFile); path != "" {
		cfg, err := policy.LoadConfig(path)
		if err != nil {
			return nil, fmt.Errorf("policy: load %s: %w", path, err)
		}
		return cfg, nil
	}
	path, err := ensureDefaultFile()
	if err != nil {
		// Couldn't write the user file (permissions, FS error). Fall
		// back to parsing the embedded default so enforcement still
		// applies — adopters notice when they try to edit and the
		// file isn't there.
		cfg, perr := policy.ParseConfig(defaultPoliciesYAML)
		if perr != nil {
			return nil, fmt.Errorf("policy: parse bundled default after seed failure %v: %w", err, perr)
		}
		return cfg, nil
	}
	cfg, err := policy.LoadConfig(path)
	if err != nil {
		return nil, fmt.Errorf("policy: load %s: %w", path, err)
	}
	return cfg, nil
}

// PoliciesPath returns the canonical user-level CEL policy path.
// Exported so dpkms doctor can surface the resolved location.
//
// Phase 2 path: $XDG_CONFIG_HOME/contexthelp/policy/ctxt.yaml.
func PoliciesPath() (string, error) {
	dir, err := xdg.RawConfigDir(xdgTool)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "policy", "ctxt.yaml"), nil
}

// LegacyPoliciesPath returns the pre-Phase-2 policy path that PR #23
// used. Exported for migration tooling and doctor checks.
//
// Legacy path: $XDG_CONFIG_HOME/contexthelp/policies.yaml.
func LegacyPoliciesPath() (string, error) {
	dir, err := xdg.RawConfigDir(xdgTool)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "policies.yaml"), nil
}

// migrateLegacyIfNeeded relocates the pre-Phase-2 policies.yaml to
// the new policy/ctxt.yaml path when:
//
//   - the new path doesn't exist, AND
//   - the legacy path does exist, AND
//   - neither file is empty
//
// The new file inherits the legacy file's bytes verbatim. The legacy
// file is removed after the move so future boots take the new path
// directly.
//
// When the new path already exists, the legacy file (if any) is left
// alone — the new path is the source of truth and a stale legacy file
// is the operator's to clean up.
func migrateLegacyIfNeeded(newPath string) error {
	if _, err := os.Stat(newPath); err == nil {
		return nil // new path already populated; nothing to migrate
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", newPath, err)
	}

	legacy, err := LegacyPoliciesPath()
	if err != nil {
		return err
	}
	st, err := os.Stat(legacy)
	switch {
	case err != nil && os.IsNotExist(err):
		return nil // no legacy file; fresh install
	case err != nil:
		return fmt.Errorf("stat legacy %s: %w", legacy, err)
	case st.Size() == 0:
		return nil // empty legacy file isn't worth migrating
	}

	body, err := os.ReadFile(legacy)
	if err != nil {
		return fmt.Errorf("read legacy %s: %w", legacy, err)
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o750); err != nil {
		return fmt.Errorf("mkdir for %s: %w", newPath, err)
	}
	if err := os.WriteFile(newPath, body, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", newPath, err)
	}
	if err := os.Remove(legacy); err != nil {
		return fmt.Errorf("remove legacy %s: %w", legacy, err)
	}
	return nil
}

// ensureDefaultFile resolves the canonical policy path, migrates from
// the legacy location if needed, and seeds the bundled default when
// the resolved path is missing or empty. Existing non-empty user
// files are left alone — never clobber adopter-authored rules.
func ensureDefaultFile() (string, error) {
	path, err := PoliciesPath()
	if err != nil {
		return "", err
	}
	if err := migrateLegacyIfNeeded(path); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", fmt.Errorf("create config directory for policies: %w", err)
	}
	st, err := os.Stat(path)
	switch {
	case err == nil && st.Size() > 0:
		return path, nil
	case err != nil && !os.IsNotExist(err):
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.WriteFile(path, defaultPoliciesYAML, 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

// ctxtPrincipalResolver picks principal from ctx → KIT_POLICY_ROLE env
// → $USER. Aps profile lookup is a deliberate follow-up — kit cannot
// import aps directly. The default kit resolver suffices today;
// ctxtPrincipalResolver exists as the seam where aps resolution lands.
func ctxtPrincipalResolver(ctx context.Context) policy.Principal {
	return policy.DefaultPrincipalResolver(ctx)
}

// DefaultPoliciesYAML returns the bundled policy bytes for callers
// that want to surface the default in `dpkms doctor` or similar.
func DefaultPoliciesYAML() []byte {
	out := make([]byte, len(defaultPoliciesYAML))
	copy(out, defaultPoliciesYAML)
	return out
}
