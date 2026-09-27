//go:build unix

// Package enqueue_test drives a built ctxt against a built, protected
// `dpkms serve`: analyze and bare `ctxt` enqueue on the one resolved
// instance or fail loudly. A writer token enqueues, a reader or unknown
// token exits 5 with nothing retried, an unreachable instance exits 70,
// and no failure leaves a database on the client. Skipped under -short.
package enqueue_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// client is a ctxt user on its own hermetic HOME/XDG tree, apart from
// the dpkms instance's, so any database it created would show.
type client struct {
	root string
	env  []string
}

func newClient(t *testing.T, config string) *client {
	t.Helper()
	root := t.TempDir()
	c := &client{root: root}
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k == "HOME" || strings.HasPrefix(k, "XDG_") || strings.HasPrefix(k, "CTXT_") {
			continue
		}
		c.env = append(c.env, kv)
	}
	c.env = append(c.env,
		"HOME="+filepath.Join(root, "home"),
		"XDG_CONFIG_HOME="+filepath.Join(root, "config"),
		"XDG_DATA_HOME="+filepath.Join(root, "data"),
		"XDG_STATE_HOME="+filepath.Join(root, "state"),
		"XDG_CACHE_HOME="+filepath.Join(root, "cache"),
		"CTXT_NO_CLIPBOARD=1",
	)
	path := filepath.Join(root, "config", "contexthelp", "ctxt.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return c
}

// run runs the built ctxt with stdin (may be "") and returns combined
// output and the exit code.
func (c *client) run(t *testing.T, stdin string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(ctxtBinary(t), args...) // #nosec G204 -- the ctxt built from this checkout
	cmd.Dir = c.root
	cmd.Env = c.env
	cmd.Stdin = strings.NewReader(stdin)
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

// assertNoDB fails when any database file exists in the client's tree.
func (c *client) assertNoDB(t *testing.T) {
	t.Helper()
	_ = filepath.WalkDir(c.root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (strings.HasPrefix(d.Name(), "db.sqlite") || strings.HasSuffix(d.Name(), ".db")) {
			t.Errorf("client created a database file: %s", path)
		}
		return nil
	})
}

var jobIDRe = regexp.MustCompile(`Job ID: (\S+)`)

func serverConfig(url, token string) string {
	return fmt.Sprintf("server:\n  url: %s\n  token: %q\n", url, token)
}

func TestEnqueueAgainstProtectedDpkms(t *testing.T) {
	d := testutil.StartDpkms(t)

	enqueues := []struct {
		name  string
		stdin string
		args  []string
	}{
		{"analyze", "", []string{"analyze", "e2e enqueued text"}},
		{"bare content", "e2e enqueued text", nil},
	}
	for _, tc := range enqueues {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("writer token enqueues on the instance", func(t *testing.T) {
				c := newClient(t, serverConfig(d.URL, d.Token(dpkmstest.RoleWriter)))
				out, code := c.run(t, tc.stdin, tc.args...)
				if code != 0 {
					t.Fatalf("exit %d; want 0\n%s", code, out)
				}
				m := jobIDRe.FindStringSubmatch(out)
				if m == nil {
					t.Fatalf("no Job ID in output:\n%s", out)
				}
				assertJobOnInstance(t, d, m[1])
				c.assertNoDB(t)
			})
			for _, role := range []string{dpkmstest.RoleReader, "unknown"} {
				t.Run(role+" token exits 5", func(t *testing.T) {
					token := d.Token(role)
					if token == "" {
						token = "not-a-token"
					}
					c := newClient(t, serverConfig(d.URL, token))
					out, code := c.run(t, tc.stdin, tc.args...)
					if code != 5 {
						t.Fatalf("exit %d; want 5\n%s", code, out)
					}
					if strings.Contains(out, "Job ID:") || strings.Contains(strings.ToLower(out), "queued locally") {
						t.Errorf("output claims an enqueue:\n%s", out)
					}
					c.assertNoDB(t)
				})
			}
			t.Run("unreachable first entry exits 70, no failover", func(t *testing.T) {
				c := newClient(t, "server:\n  token: "+d.Token(dpkmstest.RoleWriter)+"\n  urls:\n"+
					"    - url: "+testguard.ClosedServerURL+"\n"+
					"    - url: "+d.URL+"\n")
				out, code := c.run(t, tc.stdin, tc.args...)
				if code != 70 {
					t.Fatalf("exit %d; want 70\n%s", code, out)
				}
				if strings.Contains(out, "Job ID:") || strings.Contains(strings.ToLower(out), "queued locally") {
					t.Errorf("output claims an enqueue:\n%s", out)
				}
				c.assertNoDB(t)
			})
		})
	}
}

// assertJobOnInstance reads the job back from the instance as admin.
func assertJobOnInstance(t *testing.T, d *testutil.Dpkms, jobID string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, d.URL+"/api/v1/jobs/"+jobID, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+d.Token(dpkmstest.RoleAdmin))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET job %s: %v", jobID, err)
	}
	defer resp.Body.Close()
	var job struct {
		ID string `json:"id"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&job) != nil || job.ID != jobID {
		t.Fatalf("job %s not on the instance: status %d, %+v", jobID, resp.StatusCode, job)
	}
}
