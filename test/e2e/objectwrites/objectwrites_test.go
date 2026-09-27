//go:build unix

// Package objectwrites_test drives the built ctxt against a built,
// protected `dpkms serve`: edit, reprocess and delete work over the API
// only, with the exit codes ADR-077 fixes, kit's confirmation gate on
// delete, and delete reserved to admin tokens. The reprocess job runs on
// the daemon's worker pool. Skipped under -short.
package objectwrites_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

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

// run runs the built ctxt as role against d and returns its combined
// output and exit code.
func run(t *testing.T, d *testutil.Dpkms, role string, args ...string) (string, int) {
	t.Helper()
	d.WriteCtxtConfig(t, role)
	cmd := exec.Command(ctxtBinary(t), args...) // #nosec G204 -- the ctxt built from this checkout
	cmd.Dir = d.Root
	cmd.Env = d.Env
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

func expect(t *testing.T, d *testutil.Dpkms, role string, want int, args ...string) string {
	t.Helper()
	out, code := run(t, d, role, args...)
	if code != want {
		t.Fatalf("ctxt %s as %s: exit %d; want %d\n%s", strings.Join(args, " "), role, code, want, out)
	}
	return out
}

// api sends an admin request to d and decodes a JSON answer into out.
func api(t *testing.T, d *testutil.Dpkms, method, path, body string, out any) int {
	t.Helper()
	var rd *strings.Reader
	if body != "" {
		rd = strings.NewReader(body)
	} else {
		rd = strings.NewReader("")
	}
	req, err := http.NewRequest(method, d.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+d.Token(dpkmstest.RoleAdmin))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("%s %s: decode: %v", method, path, err)
		}
	}
	return resp.StatusCode
}

type job struct {
	Status   string `json:"status"`
	ResultID string `json:"result_id"`
	Error    string `json:"error"`
}

// awaitJob polls the job until it settles.
func awaitJob(t *testing.T, d *testutil.Dpkms, id string) job {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		var j job
		if code := api(t, d, http.MethodGet, "/api/v1/jobs/"+id, "", &j); code != http.StatusOK {
			t.Fatalf("GET job %s: %d", id, code)
		}
		if j.Status == "completed" || j.Status == "failed" {
			return j
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("job %s did not settle\n%s", id, d.Output())
	return job{}
}

// ingest analyzes content on d and returns the stored object's ID.
func ingest(t *testing.T, d *testutil.Dpkms, content string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"content": content, "type": "text", "source": "e2e"})
	var out struct {
		JobID string `json:"job_id"`
	}
	if code := api(t, d, http.MethodPost, "/api/v1/analyze", string(body), &out); code != http.StatusAccepted {
		t.Fatalf("analyze: %d", code)
	}
	j := awaitJob(t, d, out.JobID)
	if j.Status != "completed" || j.ResultID == "" {
		t.Fatalf("ingest job: %+v", j)
	}
	return j.ResultID
}

type object struct {
	ID        string   `json:"id"`
	Subtype   string   `json:"subtype"`
	Summaries []string `json:"summaries"`
}

var tokenRE = regexp.MustCompile(`--confirm-token=([0-9a-f]+)`)

func TestObjectWritesAgainstProtectedDpkms(t *testing.T) {
	d := testutil.StartDpkms(t)
	id := ingest(t, d, "Kubernetes clusters schedule pods onto nodes. Kubernetes nodes run the pods the scheduler assigns, and the control plane keeps them healthy.")
	const writer, reader, admin = dpkmstest.RoleWriter, dpkmstest.RoleReader, dpkmstest.RoleAdmin

	t.Run("edit", func(t *testing.T) {
		expect(t, d, reader, 5, "edit", id, "--subtype", "denied")
		expect(t, d, writer, 2, "edit", id)
		expect(t, d, writer, 2, "edit", id, "--title", "a", "--summary", "b")
		expect(t, d, writer, 3, "edit", "obj-missing", "--title", "x")
		expect(t, d, writer, 0, "edit", id, "--title", "E2E title", "--subtype", "e2e")
		var obj object
		if code := api(t, d, http.MethodGet, "/api/v1/objects/"+id, "", &obj); code != http.StatusOK {
			t.Fatalf("GET object: %d", code)
		}
		if obj.Subtype != "e2e" || len(obj.Summaries) != 1 || obj.Summaries[0] != "E2E title" {
			t.Fatalf("object after edit = %+v", obj)
		}
	})

	t.Run("reprocess runs on the daemon", func(t *testing.T) {
		expect(t, d, reader, 5, "reprocess", id)
		expect(t, d, writer, 2, "reprocess", id, "--step", "summarizer")
		out := expect(t, d, writer, 0, "reprocess", id, "--step", "tagger", "--format", "json")
		var q struct {
			JobID string `json:"job_id"`
		}
		brace := strings.Index(out, "{")
		if brace < 0 {
			t.Fatalf("reprocess printed no JSON:\n%s", out)
		}
		if err := json.Unmarshal([]byte(out[brace:]), &q); err != nil || q.JobID == "" {
			t.Fatalf("reprocess output: %v\n%s", err, out)
		}
		j := awaitJob(t, d, q.JobID)
		if j.Status != "completed" || j.ResultID != id {
			t.Fatalf("reprocess job = %+v", j)
		}
	})

	t.Run("delete: confirm gate, admin only", func(t *testing.T) {
		exists := func() bool {
			return api(t, d, http.MethodGet, "/api/v1/objects/"+id, "", nil) == http.StatusOK
		}

		// The gate refuses before anything is sent and names the token.
		out, code := run(t, d, admin, "delete", "--id", id, "--confirm=yes")
		m := tokenRE.FindStringSubmatch(out)
		if code == 0 || m == nil || !exists() {
			t.Fatalf("delete without a token: exit %d, exists %v\n%s", code, exists(), out)
		}
		token := "--confirm-token=" + m[1]

		expect(t, d, writer, 5, "delete", "--id", id, "--confirm=yes", token)
		if !exists() {
			t.Fatal("a writer deleted the object")
		}
		plan := expect(t, d, writer, 0, "delete", "--id", id, "--dry-run")
		if !strings.Contains(plan, id) || !exists() {
			t.Fatalf("preview:\n%s", plan)
		}

		expect(t, d, admin, 0, "delete", "--id", id, "--confirm=yes", token)
		if exists() {
			t.Fatal("admin delete left the object")
		}
		expect(t, d, admin, 3, "delete", "--id", id, "--confirm=yes", token)
	})
}
