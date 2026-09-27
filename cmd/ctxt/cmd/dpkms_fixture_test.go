package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
	"hop.top/kit/go/console/output"
)

// TestFixtureServesSeededObjectOverAPI: an object seeded through the test
// driver reads back through GET /api/v1/objects/{id} on the in-process
// dpkms setupTestDB starts.
func TestFixtureServesSeededObjectOverAPI(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithStaticTokens())
	now := time.Now().Truncate(time.Second)
	if err := db.Driver.Objects().Create(context.Background(), &storage.KnowledgeObject{
		ID: "obj_fixture_api", Type: "text", Summaries: []string{"seeded through the driver"},
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	resp := db.Server.Request(t, http.MethodGet, "/api/v1/objects/obj_fixture_api", dpkmstest.RoleReader, nil)
	defer resp.Body.Close()
	var got storage.KnowledgeObject
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.StatusCode != http.StatusOK || got.ID != "obj_fixture_api" || len(got.Summaries) != 1 {
		t.Fatalf("GET /api/v1/objects/obj_fixture_api: %d %+v", resp.StatusCode, got)
	}
}

// TestFixtureConfigRoutesCommandsToInstance: the config setupTestDB
// writes routes a server-bound command at the in-process instance, and
// the unreachable variant fails as nothing answering (exit 70).
func TestFixtureConfigRoutesCommandsToInstance(t *testing.T) {
	db := setupTestDB(t)
	if _, err := db.exec("status"); err != nil {
		t.Fatalf("status against the fixture: %v", err)
	}

	down := setupTestDB(t, dpkmstest.Unreachable())
	_, err := down.exec("status")
	var ke *output.Error
	if !errors.As(err, &ke) || ke.Code != output.CodePrerequisite {
		t.Fatalf("status against the unreachable fixture: err = %v; want PREREQUISITE", err)
	}
}

// TestFixtureUseRoleSwitchesToken: useRole rewrites the token commands
// send; each is the instance's token for that role.
func TestFixtureUseRoleSwitchesToken(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithStaticTokens())
	for _, role := range dpkmstest.Roles {
		db.useRole(t, role)
		body, err := os.ReadFile(db.ClientConfigPath)
		if err != nil {
			t.Fatal(err)
		}
		if want := "token: " + db.Server.Token(role) + "\n"; !strings.Contains(string(body), want) {
			t.Errorf("useRole(%s): config lacks %q:\n%s", role, want, body)
		}
	}
}

// proofChildEnv marks the child run of TestFixtureDefaultPortRequestFailsRun.
// No CTXT_/CH_/DPKMS_ prefix: testguard clears those.
const proofChildEnv = "TESTGUARD_PROOF_CHILD"

// TestFixtureDefaultPortRequestFailsRun proves that a fixture-using test
// that sends a request to the default local server (127.0.0.1:8080) fails
// the whole test run, even when the test itself passes. It re-runs this
// test binary with only this test selected; in that child the test starts
// the fixture and issues the request, which the dial guard refuses before
// connecting.
func TestFixtureDefaultPortRequestFailsRun(t *testing.T) {
	if os.Getenv(proofChildEnv) == "1" {
		// Never let a broken guard turn the proof into a real request.
		if testguard.Active == nil || !testguard.Guarded("127.0.0.1:8080") {
			t.Fatal("proof child: 127.0.0.1:8080 is not guarded; refusing to send the request")
		}
		setupTestDB(t, dpkmstest.WithStaticTokens())
		resp, err := http.Get("http://127.0.0.1:8080/health")
		if resp != nil {
			_ = resp.Body.Close()
		}
		if err == nil {
			t.Fatal("proof child: request to 127.0.0.1:8080 was not refused")
		}
		return
	}

	child := exec.Command(os.Args[0], "-test.run=^TestFixtureDefaultPortRequestFailsRun$", "-test.count=1") // #nosec G204 -- re-runs this test binary
	child.Env = append(os.Environ(), proofChildEnv+"=1")
	out, err := child.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() == 0 {
		t.Fatalf("child run: err = %v; want a failed run\n%s", err, out)
	}
	for _, want := range []string{"live-server guard refused 1 request(s)", "127.0.0.1:8080"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("child output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(string(out), "--- FAIL") {
		t.Errorf("the child test itself failed; the proof needs a passing test failed by the guard alone:\n%s", out)
	}
}
