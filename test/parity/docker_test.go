// Package parity contains static-analysis tests that catch drift
// between the canonical sources of truth (go.mod, top-level config)
// and their copies elsewhere in the tree (Dockerfiles, compose files,
// CI workflows). They run with the regular unit-test tier — no daemon,
// no network, no docker required.
package parity

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
	"gopkg.in/yaml.v3"
)

// repoRoot returns the absolute path of the ctxt repo root.
// The test package lives at <root>/test/parity, so we walk up two levels.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

// canonicalGoMajorMinor parses go.mod and returns the "1.26"-style
// major.minor of the Go directive. Patch versions are ignored because
// Docker base images publish only major.minor tags (golang:1.26-bookworm).
func canonicalGoMajorMinor(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	mf, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatalf("parse go.mod: %v", err)
	}
	if mf.Go == nil {
		t.Fatalf("go.mod has no go directive")
	}
	parts := strings.SplitN(mf.Go.Version, ".", 3)
	if len(parts) < 2 {
		t.Fatalf("malformed go directive %q", mf.Go.Version)
	}
	return parts[0] + "." + parts[1]
}

// dockerSources lists every file in the repo that pins a Go toolchain
// version. Add new Dockerfiles or compose files here when introducing them.
func dockerSources() []string {
	return []string{
		"Dockerfile",
		"docker/Dockerfile.dpkms",
		"docker-compose.yml",
	}
}

// composeFiles lists every docker-compose file in the repo.
func composeFiles() []string {
	return []string{
		"docker-compose.yml",
		"docker-compose.dev.yml",
		"docker/docker-compose.yml",
	}
}

var (
	goImageRE = regexp.MustCompile(`(?m)\bgolang:(\d+\.\d+)(?:[-.][^\s"']*)?`)
	versionRE = regexp.MustCompile(`(?m)^version:\s*['"]?\d`)
)

// TestGoVersionParity ensures every Dockerfile and compose file pins
// the same Go major.minor as go.mod. Drift here means the dev/prod
// container builds with a different toolchain than the developer's
// local `go build`, which silently breaks generics, stdlib APIs, etc.
func TestGoVersionParity(t *testing.T) {
	want := canonicalGoMajorMinor(t)
	root := repoRoot(t)

	for _, rel := range dockerSources() {
		path := filepath.Join(root, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		matches := goImageRE.FindAllStringSubmatch(string(data), -1)
		if len(matches) == 0 {
			// File doesn't pin a Go version (e.g. a runtime-only image).
			// Skip silently rather than flag — the source list is
			// authored manually, so absence is intentional.
			continue
		}
		for _, m := range matches {
			if m[1] != want {
				t.Errorf("%s: pins golang:%s, want golang:%s (from go.mod)",
					rel, m[1], want)
			}
		}
	}
}

// TestDockerComposeNoObsoleteVersionField rejects the obsolete top-level
// `version:` field. Compose v2 ignores it and emits a warning on every
// invocation; keeping it produces noise and signals a stale file.
func TestDockerComposeNoObsoleteVersionField(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range composeFiles() {
		path := filepath.Join(root, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if versionRE.MatchString(string(data)) {
			t.Errorf("%s: contains obsolete top-level `version:` field — remove it (Compose v2 ignores it)", rel)
		}
	}
}

// composeDoc is the minimal shape needed to inspect networks declared
// at the top level of a Compose file. We don't model services or
// volumes — they're irrelevant here.
type composeDoc struct {
	Networks map[string]composeNetwork `yaml:"networks"`
}

type composeNetwork struct {
	Name   string `yaml:"name"`
	Driver string `yaml:"driver"`
}

// TestDockerComposeNetworkParity ensures the project-wide network name
// is consistent across compose files. A fork between names (e.g. one
// file declaring `ctxt`, another `contexthelp`) means services started
// from different compose files land on disjoint Docker networks and
// cannot reach each other by service name.
//
// We only check files that actually declare a top-level network
// (compose files defining only opt-in side-services with profiles may
// omit the networks block entirely; that's fine).
func TestDockerComposeNetworkParity(t *testing.T) {
	const wantNetwork = "ctxt"
	root := repoRoot(t)

	for _, rel := range composeFiles() {
		path := filepath.Join(root, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		var doc composeDoc
		if err := yaml.Unmarshal(data, &doc); err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}
		for key, net := range doc.Networks {
			// `name:` overrides the map key as the actual Docker
			// network name; if absent, the key wins.
			got := net.Name
			if got == "" {
				got = key
			}
			if got != wantNetwork {
				t.Errorf("%s: network %q resolves to Docker name %q, want %q",
					rel, key, got, wantNetwork)
			}
		}
	}
}
