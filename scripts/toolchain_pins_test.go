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
			compose := "services:\n  app:\n    image: " + tc.image + "\n  db:\n    image: postgres:16\n"
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
