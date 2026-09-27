//go:build unix

// Package inbox_test drives a built ctxt's inbox commands against a
// built, protected `dpkms serve`: reading needs read:inbox, triage,
// discard and clear need process:inbox (admin only) and exit 5 without
// it, and discard and clear stay behind kit's confirm policy. Skipped
// under -short.
package inbox_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
	"github.com/ideacrafterslabs/ctxt/test/testutil"
)

func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil, "ctxt", "dpkms"))
}

var (
	ctxtOnce sync.Once
	ctxtPath string
	ctxtErr  error
)

// ctxtBinary builds ./cmd/ctxt from this checkout once per test binary.
func ctxtBinary(t *testing.T) string {
	t.Helper()
	ctxtOnce.Do(func() {
		root, err := moduleRoot()
		if err != nil {
			ctxtErr = err
			return
		}
		dir, err := os.MkdirTemp(os.Getenv("XDG_CACHE_HOME"), "ctxt-e2e-")
		if err != nil {
			ctxtErr = err
			return
		}
		ctxtPath = filepath.Join(dir, "ctxt")
		build := exec.Command("go", "build", "-tags", "fts5", "-buildvcs=false", "-o", ctxtPath, "./cmd/ctxt") // #nosec G204 -- fixed args; fresh temp path
		build.Dir = root
		build.Env = append(os.Environ(), "CGO_ENABLED=1")
		if out, err := build.CombinedOutput(); err != nil {
			ctxtErr = fmt.Errorf("go build ./cmd/ctxt: %w\n%s", err, out)
		}
	})
	if ctxtErr != nil {
		t.Fatal(ctxtErr)
	}
	return ctxtPath
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found")
		}
		dir = parent
	}
}

// ctxtAs runs the built binary as role against d and returns its
// combined output and exit code.
func ctxtAs(t *testing.T, d *testutil.Dpkms, role string, args ...string) (string, int) {
	t.Helper()
	d.WriteCtxtConfig(t, role)
	cmd := exec.Command(ctxtBinary(t), args...) // #nosec G204 -- the ctxt built from this checkout
	cmd.Dir = d.Root
	cmd.Env = d.Env
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0
	case errors.As(err, &exit):
		return string(out), exit.ExitCode()
	default:
		t.Fatalf("run ctxt %v: %v", args, err)
		return "", -1
	}
}

// capture adds an inbox item through the API as a writer and returns its
// ID.
func capture(t *testing.T, d *testutil.Dpkms, content string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"content": content, "type": "text"})
	req, err := http.NewRequest(http.MethodPost, d.URL+"/api/v1/inbox", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+d.Token(dpkmstest.RoleWriter))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var obj struct {
		ID string `json:"id"`
	}
	if resp.StatusCode != http.StatusCreated || json.NewDecoder(resp.Body).Decode(&obj) != nil || obj.ID == "" {
		t.Fatalf("capture to inbox: %d", resp.StatusCode)
	}
	return obj.ID
}

// confirmToken is the --confirm-token kit's typed-confirmation gate
// expects for a destructive command path.
func confirmToken(path string) string {
	h := sha256.Sum256([]byte(path))
	return "--confirm-token=" + hex.EncodeToString(h[:6])
}

func TestInboxOverAPIAgainstProtectedDpkms(t *testing.T) {
	discardOK := confirmToken("ctxt inbox discard")
	clearOK := confirmToken("ctxt inbox clear")
	d := testutil.StartDpkms(t)
	first := capture(t, d, "first inbox item")
	second := capture(t, d, "second inbox item")

	expect := func(t *testing.T, role string, want int, args ...string) string {
		t.Helper()
		out, code := ctxtAs(t, d, role, args...)
		if code != want {
			t.Fatalf("%s: ctxt %s: exit %d; want %d\n%s", role, strings.Join(args, " "), code, want, out)
		}
		return out
	}

	t.Run("reader lists the inbox and the queue", func(t *testing.T) {
		out := expect(t, dpkmstest.RoleReader, 0, "inbox", "list")
		if !strings.Contains(out, first) || !strings.Contains(out, "2 total") {
			t.Fatalf("inbox list:\n%s", out)
		}
		expect(t, dpkmstest.RoleReader, 0, "inbox", "list", "--pending")
	})
	t.Run("writer is refused processing with exit 5", func(t *testing.T) {
		// Past the confirm gate, so the refusal is the server's 403.
		for _, args := range [][]string{
			{"inbox", "triage", first},
			{"inbox", "discard", discardOK, first},
			{"inbox", "clear", clearOK},
		} {
			out := expect(t, dpkmstest.RoleWriter, 5, args...)
			if !strings.Contains(out, "process:inbox") {
				t.Fatalf("ctxt %s: refusal does not name the missing scope:\n%s", strings.Join(args, " "), out)
			}
		}
		out := expect(t, dpkmstest.RoleReader, 0, "inbox", "list")
		if !strings.Contains(out, "2 total") {
			t.Fatalf("a refused command changed the inbox:\n%s", out)
		}
	})
	t.Run("no token exits 5", func(t *testing.T) {
		expect(t, "none", 5, "inbox", "list")
	})
	t.Run("discard and clear need the confirm token", func(t *testing.T) {
		for _, args := range [][]string{
			{"inbox", "clear"},
			{"inbox", "clear", "--confirm=yes"},
			{"inbox", "discard", "--confirm=yes", first},
		} {
			out := expect(t, dpkmstest.RoleAdmin, 5, args...)
			if !strings.Contains(out, "--confirm-token=") {
				t.Fatalf("ctxt %s: not refused by the confirm gate:\n%s", strings.Join(args, " "), out)
			}
		}
		list := expect(t, dpkmstest.RoleReader, 0, "inbox", "list")
		if !strings.Contains(list, "2 total") {
			t.Fatalf("an unconfirmed command changed the inbox:\n%s", list)
		}
	})
	t.Run("admin triages and clears", func(t *testing.T) {
		out := expect(t, dpkmstest.RoleAdmin, 0, "inbox", "triage", first)
		if !strings.Contains(out, first) {
			t.Fatalf("triage:\n%s", out)
		}
		out = expect(t, dpkmstest.RoleAdmin, 0, "inbox", "clear", clearOK)
		if !strings.Contains(out, "Cleared 1 inbox item(s)") {
			t.Fatalf("clear:\n%s", out)
		}
		out = expect(t, dpkmstest.RoleReader, 0, "inbox", "list")
		if !strings.Contains(out, "0 total") || strings.Contains(out, second) {
			t.Fatalf("inbox after clear:\n%s", out)
		}
		expect(t, dpkmstest.RoleAdmin, 3, "inbox", "discard", discardOK, "no-such-item")
	})
	t.Run("triage and discard refuse what left the inbox with exit 3", func(t *testing.T) {
		// first is active (triaged), second discarded (cleared).
		for id, status := range map[string]string{first: "active", second: "discarded"} {
			for _, args := range [][]string{
				{"inbox", "triage", id},
				{"inbox", "discard", discardOK, id},
			} {
				out := expect(t, dpkmstest.RoleAdmin, 3, args...)
				if !strings.Contains(out, "no inbox item "+id) || !strings.Contains(out, "ctxt inbox list") {
					t.Fatalf("ctxt %s: unclear refusal:\n%s", strings.Join(args, " "), out)
				}
			}
			if got := objectStatus(t, d, id); got != status {
				t.Fatalf("%s: status %q after refused triage/discard; want %q", id, got, status)
			}
		}
	})
}

// objectStatus reads object id's status through the API as a reader.
func objectStatus(t *testing.T, d *testutil.Dpkms, id string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, d.URL+"/api/v1/objects/"+id, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+d.Token(dpkmstest.RoleReader))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var obj struct {
		Status string `json:"status"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&obj) != nil {
		t.Fatalf("get object %s: %d", id, resp.StatusCode)
	}
	return obj.Status
}
