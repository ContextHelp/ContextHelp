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
		{"exact off pin", "docker-compose.yml", "golang:1.0.0-bookworm", false},
		{"exact off pin with digest", "docker-compose.yml", "golang:1.0.0@sha256:" + strings.Repeat("a", 64), false},
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

// Workflow jobs pull images too: service containers, a job's container
// (shorthand or mapping) and `docker://` steps. They follow the compose
// rule, and a golang image must run the mise.toml Go pin.
func TestCheckToolchainPins_WorkflowImage(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	mise := readMise(t)
	pin := misePin(t, mise)

	service := func(img string) string {
		return "    services:\n      db:\n        image: " + img + "\n        ports:\n          - 5432:5432\n"
	}
	container := func(img string) string { return "    container: " + img + "\n" }
	containerMap := func(img string) string {
		return "    container:\n      image: " + img + "\n      options: --cpus 1\n"
	}
	dockerStep := func(img string) string { return "      - uses: docker://" + img + "\n" }

	cases := []struct {
		name, file, job, step string
		ok                    bool
	}{
		{"service exact", "ci.yml", service("pgvector/pgvector:0.8.6-pg16-bookworm"), "", true},
		{"service exact redis", "ci.yml", service("redis:7.4.11-alpine3.21"), "", true},
		{"service digest", "ci.yml", service("redis@sha256:" + strings.Repeat("a", 64)), "", true},
		{"container exact", "ci.yml", container("debian:bookworm-20260918-slim"), "", true},
		{"container mapping exact", "ci.yml", containerMap("alpine:3.22.2"), "", true},
		{"docker step exact", "ci.yml", "", dockerStep("alpine:3.22.2"), true},
		{"golang at pin", "ci.yml", container("golang:" + pin + "-bookworm"), "", true},
		{"commented out", "ci.yml", "    # services:\n    #   db:\n    #     image: redis:7-alpine\n", "", true},
		{"no images", "ci.yml", "", "", true},
		{"service floating", "release.yml", service("postgres:16-alpine"), "", false},
		{"service major only", "release.yml", service("redis:7-alpine"), "", false},
		{"service quoted floating", "ci.yml", service(`'redis:7-alpine'`), "", false},
		{"service latest", "ci.yml", service("redis:latest"), "", false},
		{"service no tag", "ci.yml", service("redis"), "", false},
		{"service expression", "ci.yml", service("${{ matrix.image }}"), "", false},
		{"container floating", "ci.yml", container("node:22-alpine"), "", false},
		{"container mapping floating", "ci.yml", containerMap("debian:bookworm-slim"), "", false},
		{"docker step floating", "ci.yml", "", dockerStep("alpine:3"), false},
		{"golang off pin", "ci.yml", container("golang:1.0.0-bookworm"), "", false},
		{"golang registry path off pin", "ci.yml", containerMap("docker.io/library/golang:1.0.0"), "", false},
		{"yaml extension", "nightly.yaml", service("redis:7-alpine"), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := pinsRepo(t, mise)
			wf := filepath.Join(".github", "workflows", tc.file)
			body := "name: t\non: push\njobs:\n  j:\n    runs-on: ubuntu-latest\n" + tc.job +
				"    steps:\n      - run: true\n" + tc.step
			writePinsFile(t, filepath.Join(repo, wf), body)

			out, err := exec.Command("bash", filepath.Join(repo, "scripts", "check-toolchain-pins.sh")).CombinedOutput()
			if tc.ok {
				if err != nil {
					t.Fatalf("pinned workflow refused: %v\n%s\n%s", err, body, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("%s passed the gate:\n%s\n%s", wf, body, out)
			}
			if !strings.Contains(string(out), "FAIL: "+wf+" runs image ") {
				t.Errorf("failure does not name %s:\n%s", wf, out)
			}
		})
	}
}

// mise.toml pins node like go. Every place that names a Node version
// must agree with it: workflows derive node-version instead of
// hardcoding it, node images run the pin, and .nvmrc/.node-version and
// package.json engines.node do not contradict it.
func TestCheckToolchainPins_NodePin(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	mise := readMise(t)
	pin := miseNodePin(t, mise)
	major, _, _ := strings.Cut(pin, ".") // 22.23.3 -> 22

	setupNode := func(with string) string {
		return "name: t\non: push\njobs:\n  j:\n    runs-on: ubuntu-latest\n    steps:\n" +
			"      - uses: actions/setup-node@v7\n        with:\n          " + with + "\n"
	}
	nodeContainer := func(img string) string {
		return "name: t\non: push\njobs:\n  j:\n    runs-on: ubuntu-latest\n    container: " + img +
			"\n    steps:\n      - run: true\n"
	}
	pkg := func(engines string) string {
		return "{\n  \"name\": \"ui\",\n  \"private\": true,\n  \"engines\": {\n    \"node\": \"" + engines + "\"\n  }\n}\n"
	}

	cases := []struct {
		name, file, content, want string
	}{
		{"derived node-version", ".github/workflows/ci.yml", setupNode("node-version: ${{ needs.toolchain.outputs.node-version }}"), ""},
		{"literal pin", ".github/workflows/ci.yml", setupNode("node-version: '" + pin + "'"), "FAIL: workflow hardcodes a Node version"},
		{"literal major", ".github/workflows/ci.yml", setupNode("node-version: " + major), "FAIL: workflow hardcodes a Node version"},
		{"lts alias", ".github/workflows/ci.yml", setupNode("node-version: lts/*"), "FAIL: workflow hardcodes a Node version"},
		{"node-version-file", ".github/workflows/ci.yml", setupNode("node-version-file: .nvmrc"), "FAIL: workflow reads node-version-file"},
		{"workflow node image at pin", ".github/workflows/ci.yml", nodeContainer("node:" + pin + "-alpine3.24"), ""},
		{"workflow node image off pin", ".github/workflows/ci.yml", nodeContainer("node:20.19.5-alpine"), "FAIL: .github/workflows/ci.yml runs image "},
		{"compose node at pin", "docker-compose.yml", "services:\n  ui:\n    image: node:" + pin + "-alpine3.24\n", ""},
		{"compose node off pin", "docker-compose.yml", "services:\n  ui:\n    image: node:20.19.5-alpine3.24\n", "FAIL: docker-compose.yml runs image "},
		{"dockerfile node at pin", "Dockerfile", "FROM node:" + pin + "-alpine3.24 AS ui\n", ""},
		{"dockerfile node off pin", "Dockerfile", "FROM node:20.19.5-alpine3.24 AS ui\n", "FAIL: Dockerfile builds FROM "},
		{"dockerfile node ARG off pin", "Dockerfile", "ARG NODE_VERSION=20.19.5\nFROM node:${NODE_VERSION}-alpine AS ui\n", "FAIL: Dockerfile builds FROM "},
		{"nvmrc at pin", ".nvmrc", pin + "\n", ""},
		{"nvmrc v prefix", "web/ui/.nvmrc", "v" + pin + "\n", ""},
		{"nvmrc major", ".nvmrc", major + "\n", "FAIL: .nvmrc pins node " + major + " but mise.toml pins node " + pin},
		{"node-version file off pin", "web/ui/.node-version", "20.19.5\n", "FAIL: web/ui/.node-version pins node 20.19.5"},
		{"engines exact pin", "web/ui/package.json", pkg(pin), ""},
		{"engines minimum satisfied", "web/ui/package.json", pkg(">=" + major), ""},
		{"engines exact off pin", "web/ui/package.json", pkg("20.19.5"), "FAIL: web/ui/package.json engines.node"},
		{"engines minimum above pin", "web/ui/package.json", pkg(">=99"), "FAIL: web/ui/package.json engines.node"},
		{"engines unverifiable range", "web/ui/package.json", pkg("^" + major), "FAIL: web/ui/package.json engines.node"},
		{"no engines", "web/ui/package.json", "{\n  \"name\": \"ui\",\n  \"private\": true\n}\n", ""},
		{"node_modules ignored", "web/ui/node_modules/x/package.json", pkg("20.19.5"), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := pinsRepo(t, mise)
			writePinsFile(t, filepath.Join(repo, tc.file), tc.content)

			out, err := exec.Command("bash", filepath.Join(repo, "scripts", "check-toolchain-pins.sh")).CombinedOutput()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("%s refused: %v\n%s\n%s", tc.file, err, tc.content, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("%s passed the gate:\n%s\n%s", tc.file, tc.content, out)
			}
			if !strings.Contains(string(out), tc.want) {
				t.Errorf("failure does not say %q:\n%s", tc.want, out)
			}
		})
	}
}

// Without a node pin there is nothing to hold the others to.
func TestCheckToolchainPins_NodePinRequired(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
	mise := readMise(t)
	for name, line := range map[string]string{
		"missing":  "",
		"floating": "node = \"22\"\n",
		"latest":   "node = \"latest\"\n",
		"lts":      "node = \"lts\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			m := regexp.MustCompile(`(?m)^node = ".*"\n`).ReplaceAll(mise, []byte(line))
			repo := pinsRepo(t, m)
			out, err := exec.Command("bash", filepath.Join(repo, "scripts", "check-toolchain-pins.sh")).CombinedOutput()
			if err == nil {
				t.Fatalf("mise.toml with node line %q passed the gate\n%s", line, out)
			}
			if !strings.Contains(string(out), "FAIL: mise.toml") || !strings.Contains(string(out), "node") {
				t.Errorf("failure does not name the mise.toml node pin:\n%s", out)
			}
		})
	}
}

func miseNodePin(t *testing.T, mise []byte) string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^node = "(.+)"$`).FindSubmatch(mise)
	if m == nil {
		t.Fatal("mise.toml has no node pin")
	}
	return string(m[1])
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
