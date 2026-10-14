//go:build unix

package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The repo's own toolchain declarations agree with mise.toml: the gate
// CI runs passes on this checkout.
func TestCheckToolchainPins_RepoAgrees(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	out, err := exec.Command("bash", "check-toolchain-pins.sh").CombinedOutput()
	if err != nil {
		t.Fatalf("check-toolchain-pins.sh: %v\n%s", err, out)
	}
}

// A compose file that runs a golang image on a floating tag compiles
// with whatever patch that tag tracks today, not the mise.toml pin. The
// gate must refuse it wherever the compose file lives.
func TestCheckToolchainPins_ComposeGoImage(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	mise, err := os.ReadFile(filepath.Join("..", "mise.toml"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^go = "(.+)"$`).FindSubmatch(mise)
	if m == nil {
		t.Fatal("mise.toml has no go pin")
	}
	pin := string(m[1])
	minor := pin[:strings.LastIndex(pin, ".")] // 1.26.8 -> 1.26

	cases := []struct {
		name, file, image string
		ok                bool
	}{
		{"pinned", "docker-compose.yml", "golang:" + pin + "-bookworm", true},
		{"floating minor", "docker-compose.yml", "golang:" + minor + "-bookworm", false},
		{"quoted floating", "docker-compose.yml", `"golang:` + minor + `"`, false},
		{"latest", "docker-compose.dev.yml", "golang:latest", false},
		{"registry path", "docker-compose.yml", "docker.io/library/golang:" + minor + "-alpine", false},
		{"nested compose file", "docker/docker-compose.yml", "golang:" + minor + "-bookworm", false},
		{"devcontainer compose file", ".devcontainer/docker-compose.yml", "golang:" + minor, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := pinsRepo(t, mise)
			compose := "services:\n  app:\n    image: " + tc.image + "\n  db:\n    image: pgvector/pgvector:0.8.6-pg16-bookworm\n"
			writePinsFile(t, filepath.Join(repo, tc.file), compose)

			out, err := exec.Command("bash", filepath.Join(repo, "scripts", "check-toolchain-pins.sh")).CombinedOutput()
			if tc.ok {
				if err != nil {
					t.Fatalf("pinned image refused: %v\n%s", err, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("%s running %s passed the gate\n%s", tc.file, tc.image, out)
			}
			if !strings.Contains(string(out), "FAIL: "+tc.file+" runs image ") {
				t.Errorf("failure does not name %s:\n%s", tc.file, out)
			}
		})
	}
}

// Every other image a compose file runs must name an exact release too:
// a major or minor tag (redis:7-alpine), latest or no tag at all pulls
// whatever the registry points it at today. The project's own image
// (ctxt/*) is a local build output and is exempt.
func TestCheckToolchainPins_ComposeFloatingImage(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	mise := readMise(t)

	cases := []struct {
		name, file, image string
		ok                bool
	}{
		{"exact patch", "docker-compose.yml", "caddy:2.11.4-alpine", true},
		{"exact patch and base", "docker-compose.dev.yml", "redis:7.4.11-alpine3.21", true},
		{"quoted exact", "docker-compose.yml", `"node:22.23.3-alpine3.24"`, true},
		{"single quoted exact", "docker-compose.yml", `'node:22.23.3-alpine3.24'`, true},
		{"extension version", "docker-compose.dev.yml", "pgvector/pgvector:0.8.6-pg16-bookworm", true},
		{"dated tag", "docker-compose.yml", "debian:bookworm-20260918-slim", true},
		{"digest", "docker-compose.yml", "redis@sha256:" + strings.Repeat("a", 64), true},
		{"registry port exact", "docker-compose.yml", "registry.local:5000/redis:7.4.11", true},
		{"trailing comment", "docker-compose.yml", "redis:7.4.11-alpine # cache", true},
		{"own image", "docker-compose.yml", "ctxt/dpkms:${TAG:-latest}", true},
		{"interpolated exact default", "docker-compose.yml", "${REDIS_IMAGE:-redis:7.4.11-alpine}", true},
		{"major only", "docker-compose.yml", "caddy:2-alpine", false},
		{"major only redis", "docker-compose.dev.yml", "redis:7-alpine", false},
		{"minor only", "docker-compose.yml", "node:22.23-alpine", false},
		{"variant only", "docker-compose.dev.yml", "pgvector/pgvector:pg16", false},
		{"latest", "docker-compose.yml", "redis:latest", false},
		{"no tag", "docker-compose.yml", "redis", false},
		{"quoted floating", "docker-compose.yml", `"caddy:2-alpine"`, false},
		{"registry port no tag", "docker-compose.yml", "registry.local:5000/redis", false},
		{"interpolated floating default", "docker-compose.yml", "${REDIS_IMAGE:-redis:7-alpine}", false},
		{"interpolated no default", "docker-compose.yml", "${REDIS_IMAGE}", false},
		{"undated codename", "docker-compose.yml", "debian:bookworm-slim", false},
		{"nested compose file", "docker/docker-compose.yml", "redis:7-alpine", false},
		{"devcontainer compose file", ".devcontainer/docker-compose.yml", "redis:7-alpine", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := pinsRepo(t, mise)
			compose := "services:\n  web:\n    image: caddy:2.11.4-alpine\n  svc:\n    image: " + tc.image + "\n"
			writePinsFile(t, filepath.Join(repo, tc.file), compose)

			out, err := exec.Command("bash", filepath.Join(repo, "scripts", "check-toolchain-pins.sh")).CombinedOutput()
			if tc.ok {
				if err != nil {
					t.Fatalf("pinned image %s refused: %v\n%s", tc.image, err, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("%s running %s passed the gate\n%s", tc.file, tc.image, out)
			}
			if !strings.Contains(string(out), "FAIL: "+tc.file+" runs image ") {
				t.Errorf("failure does not name %s:\n%s", tc.file, out)
			}
		})
	}
}

// Dockerfile base images follow the same rule. ARG defaults are
// resolved, earlier stages and scratch are not pulled images.
func TestCheckToolchainPins_DockerfileFloatingBase(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	mise := readMise(t)
	pin := misePin(t, mise)
	builder := "ARG GO_VERSION=" + pin + "\nFROM golang:${GO_VERSION}-bookworm AS go-builder\n"

	cases := []struct {
		name, file, from string
		ok               bool
	}{
		{"dated runtime", "Dockerfile", "FROM debian:bookworm-20260918-slim AS runtime", true},
		{"exact runtime lowercase as", "Dockerfile", "from alpine:3.22.2 as runtime", true},
		{"earlier stage", "Dockerfile", "FROM go-builder AS runtime", true},
		{"scratch", "Dockerfile", "FROM scratch", true},
		{"platform flag", "Dockerfile", "FROM --platform=$BUILDPLATFORM node:22.23.3-alpine3.24 AS ui", true},
		{"undated codename", "Dockerfile", "FROM debian:bookworm-slim AS runtime", false},
		{"major only", "Dockerfile", "FROM node:22-alpine AS ui", false},
		{"no tag", "Dockerfile", "FROM ubuntu AS runtime", false},
		{"latest", "Dockerfile", "FROM alpine:latest", false},
		{"floating ARG default", "Dockerfile", "ARG NODE=22\nFROM node:${NODE}-alpine", false},
		{"unresolved ARG", "Dockerfile", "FROM node:${NODE_VERSION}-alpine", false},
		{"devcontainer Dockerfile", ".devcontainer/Dockerfile", "FROM node:22-alpine AS ui", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := pinsRepo(t, mise)
			writePinsFile(t, filepath.Join(repo, tc.file), builder+tc.from+"\n")

			out, err := exec.Command("bash", filepath.Join(repo, "scripts", "check-toolchain-pins.sh")).CombinedOutput()
			if tc.ok {
				if err != nil {
					t.Fatalf("pinned base %q refused: %v\n%s", tc.from, err, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("%s with %q passed the gate\n%s", tc.file, tc.from, out)
			}
			if !strings.Contains(string(out), "FAIL: "+tc.file+" builds FROM ") {
				t.Errorf("failure does not name %s:\n%s", tc.file, out)
			}
		})
	}
}

// `# syntax=` pulls the BuildKit frontend image, so it is pinned like a
// base image. Only the leading parser directives count.
func TestCheckToolchainPins_DockerfileSyntaxFrontend(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	mise := readMise(t)
	body := "FROM debian:bookworm-20260918-slim\n"

	cases := []struct {
		name, head string
		ok         bool
	}{
		{"exact", "# syntax=docker/dockerfile:1.27.0\n", true},
		{"spaced exact", "# syntax = docker/dockerfile:1.27.0\n", true},
		{"after escape directive", "# escape=`\n# syntax=docker/dockerfile:1.27.0\n", true},
		{"no directive", "", true},
		{"comment after content is not a directive", "# a comment\n# syntax=docker/dockerfile:1\n", true},
		{"major only", "# syntax=docker/dockerfile:1\n", false},
		{"minor only", "# syntax=docker/dockerfile:1.27\n", false},
		{"after escape directive floating", "# escape=`\n# syntax=docker/dockerfile:1\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := pinsRepo(t, mise)
			writePinsFile(t, filepath.Join(repo, "Dockerfile"), tc.head+body)

			out, err := exec.Command("bash", filepath.Join(repo, "scripts", "check-toolchain-pins.sh")).CombinedOutput()
			if tc.ok {
				if err != nil {
					t.Fatalf("frontend %q refused: %v\n%s", tc.head, err, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("frontend %q passed the gate\n%s", tc.head, out)
			}
			if !strings.Contains(string(out), "FAIL: Dockerfile uses frontend ") {
				t.Errorf("failure does not name the frontend:\n%s", out)
			}
		})
	}
}

func readMise(t *testing.T) []byte {
	t.Helper()
	mise, err := os.ReadFile(filepath.Join("..", "mise.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return mise
}

func misePin(t *testing.T, mise []byte) string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^go = "(.+)"$`).FindSubmatch(mise)
	if m == nil {
		t.Fatal("mise.toml has no go pin")
	}
	return string(m[1])
}

// pinsRepo lays out the smallest tree check-toolchain-pins.sh accepts:
// the script, mise.toml and a go.mod the pin satisfies.
func pinsRepo(t *testing.T, mise []byte) string {
	t.Helper()
	repo := t.TempDir()
	script, err := os.ReadFile("check-toolchain-pins.sh")
	if err != nil {
		t.Fatal(err)
	}
	writePinsFile(t, filepath.Join(repo, "scripts", "check-toolchain-pins.sh"), string(script))
	writePinsFile(t, filepath.Join(repo, "mise.toml"), string(mise))
	writePinsFile(t, filepath.Join(repo, "go.mod"), "module example.com/pins\n\ngo 1.26.1\n")
	if err := os.MkdirAll(filepath.Join(repo, ".github", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	return repo
}

func writePinsFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
