package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
)

// httpCommandCases is every command that already talks to dpkms over its
// API: the feed and import clients plus status, log, upgrade status and
// capture.
func httpCommandCases() []serverClientCase {
	return append(serverClientCases(),
		serverClientCase{"status", func(*testing.T) []string { return []string{"status"} }},
		serverClientCase{"log", func(*testing.T) []string { return []string{"log"} }},
		serverClientCase{"upgrade status", func(*testing.T) []string { return []string{"upgrade", "status"} }},
		serverClientCase{"capture", func(*testing.T) []string { return []string{"capture", "a captured line"} }},
		serverClientCase{"capture --inbox", func(*testing.T) []string {
			return []string{"capture", "an inbox line", "--inbox"}
		}},
	)
}

// namedEndpoints is a server.urls list whose first entry is a decoy and
// whose second is named home with its own token.
func namedEndpoints(decoy, home string) string {
	return "server:\n  token: tok-default\n  urls:\n" +
		"    - name: decoy\n      url: " + decoy + "\n" +
		"    - name: home\n      url: " + home + "\n      token: tok-home\n"
}

// writeCurrentInstance persists sel as `ctxt instance use` would.
func writeCurrentInstance(t *testing.T, sel string) {
	t.Helper()
	path, err := config.CurrentInstanceFile()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(sel), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestHTTPCommands_SelectNamedEntry: --instance, CTXT_INSTANCE and the
// current-instance state each route every HTTP command to the named
// server.urls entry with that entry's token, and never to the first
// entry.
func TestHTTPCommands_SelectNamedEntry(t *testing.T) {
	selectors := []struct {
		name  string
		apply func(t *testing.T, args []string) []string
	}{
		{"--instance", func(_ *testing.T, args []string) []string { return append(args, "--instance", "home") }},
		{"CTXT_INSTANCE", func(t *testing.T, args []string) []string {
			t.Setenv("CTXT_INSTANCE", "home")
			return args
		}},
		{"current-instance", func(t *testing.T, args []string) []string {
			writeCurrentInstance(t, "home")
			return args
		}},
	}
	for _, sel := range selectors {
		for _, tc := range httpCommandCases() {
			t.Run(sel.name+"/"+tc.name, func(t *testing.T) {
				decoy := newRecordedServer(t, fakeDpkmsAPI())
				home := newRecordedServer(t, fakeDpkmsAPI())
				db := setupTestDB(t)
				appendConfig(t, db, namedEndpoints(decoy.URL, home.URL))

				out, err := db.exec(sel.apply(t, tc.args(t))...)
				if err != nil {
					t.Fatalf("%s: %v\n%s", tc.name, err, out)
				}
				assertAllAuthorized(t, home.hits(), "Bearer tok-home")
				if n := len(decoy.hits()); n != 0 {
					t.Fatalf("first server.urls entry got %d requests; want 0", n)
				}
			})
		}
	}
}

// TestHTTPCommands_ServerFlagBeatsInstance: --server outranks --instance.
func TestHTTPCommands_ServerFlagBeatsInstance(t *testing.T) {
	for _, tc := range httpCommandCases() {
		t.Run(tc.name, func(t *testing.T) {
			home := newRecordedServer(t, fakeDpkmsAPI())
			pinned := newRecordedServer(t, fakeDpkmsAPI())
			db := setupTestDB(t)
			appendConfig(t, db, namedEndpoints(closedServerURL(t), home.URL))

			out, err := db.exec(append(tc.args(t), "--instance", "home", "--server", pinned.URL)...)
			if err != nil {
				t.Fatalf("%s: %v\n%s", tc.name, err, out)
			}
			assertAllAuthorized(t, pinned.hits(), "Bearer tok-default")
			if n := len(home.hits()); n != 0 {
				t.Fatalf("--instance target got %d requests under --server; want 0", n)
			}
		})
	}
}

// TestHTTPCommands_UnknownInstanceExits70: a name nothing answers to exits
// 70 (PREREQUISITE) before any request, naming the configured entries.
func TestHTTPCommands_UnknownInstanceExits70(t *testing.T) {
	for _, tc := range httpCommandCases() {
		t.Run(tc.name, func(t *testing.T) {
			decoy := newRecordedServer(t, fakeDpkmsAPI())
			home := newRecordedServer(t, fakeDpkmsAPI())
			db := setupTestDB(t)
			appendConfig(t, db, namedEndpoints(decoy.URL, home.URL))

			out, err := db.exec(append(tc.args(t), "--instance", "nope")...)
			if got := ExitCodeFor(err); got != output.ExitPrerequisite {
				t.Fatalf("exit %d (%v); want %d\n%s", got, err, output.ExitPrerequisite, out)
			}
			var e *output.Error
			if !asEnvelope(err, &e) || !strings.Contains(e.SuggestedFix, "decoy, home") {
				t.Fatalf("error %v does not list the configured names", err)
			}
			if n := len(decoy.hits()) + len(home.hits()); n != 0 {
				t.Fatalf("an unknown name sent %d requests; want 0", n)
			}
		})
	}
}

// TestHTTPCommands_StaleCurrentInstanceExits70: a persisted selection
// that no longer resolves is an error, not a fall-through to server.urls.
func TestHTTPCommands_StaleCurrentInstanceExits70(t *testing.T) {
	decoy := newRecordedServer(t, fakeDpkmsAPI())
	db := setupTestDB(t)
	appendConfig(t, db, namedEndpoints(decoy.URL, decoy.URL))
	writeCurrentInstance(t, "gone")

	out, err := db.exec("status")
	if got := ExitCodeFor(err); got != output.ExitPrerequisite {
		t.Fatalf("exit %d (%v); want %d\n%s", got, err, output.ExitPrerequisite, out)
	}
	var e *output.Error
	if !asEnvelope(err, &e) || !strings.Contains(e.SuggestedFix, "ctxt instance use -") {
		t.Fatalf("error %v does not say how to clear the selection", err)
	}
	if n := len(decoy.hits()); n != 0 {
		t.Fatalf("stale selection sent %d requests; want 0", n)
	}
}

// TestHTTPCommands_NoFailover: an unreachable first server.urls entry
// fails the command; the next entry is never tried.
func TestHTTPCommands_NoFailover(t *testing.T) {
	for _, tc := range httpCommandCases() {
		t.Run(tc.name, func(t *testing.T) {
			second := newRecordedServer(t, fakeDpkmsAPI())
			db := setupTestDB(t)
			appendConfig(t, db, namedEndpoints(closedServerURL(t), second.URL))

			out, err := db.exec(tc.args(t)...)
			if err == nil {
				t.Fatalf("%s succeeded with its endpoint down\n%s", tc.name, out)
			}
			if n := len(second.hits()); n != 0 {
				t.Fatalf("second server.urls entry got %d requests; want 0 (no failover)", n)
			}
		})
	}
}

// TestHTTPCommands_UnreachableExits70: a single-shot command against an
// endpoint that does not answer exits 70.
func TestHTTPCommands_UnreachableExits70(t *testing.T) {
	for _, args := range [][]string{
		{"status"}, {"log"}, {"upgrade", "status"}, {"feed", "list"},
		{"import", "batch", "status", "b1"}, {"capture", "a line"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			db := setupTestDB(t, dpkmstest.Unreachable())
			out, err := db.exec(args...)
			if got := ExitCodeFor(err); got != output.ExitPrerequisite {
				t.Fatalf("exit %d (%v); want %d\n%s", got, err, output.ExitPrerequisite, out)
			}
		})
	}
}

// TestStatus_InstanceAgainstProtectedDpkms: `ctxt status --instance home`
// and `ctxt log --instance home` reach a protected in-process dpkms with
// the named entry's token; a wrong token for the same URL exits 5.
func TestStatus_InstanceAgainstProtectedDpkms(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithStaticTokens())
	appendConfig(t, db, "server:\n  urls:\n"+
		"    - name: home\n      url: "+db.Server.URL+"\n      token: "+db.Server.Token(dpkmstest.RoleAdmin)+"\n"+
		"    - name: stale\n      url: "+db.Server.URL+"/\n      token: not-a-token\n")

	for _, args := range [][]string{{"status"}, {"log"}} {
		out, err := db.exec(append(args, "--instance", "home")...)
		if err != nil {
			t.Fatalf("%v --instance home: %v\n%s", args, err, out)
		}
	}
	out, err := db.exec("log", "--instance", "stale")
	if got := ExitCodeFor(err); got != output.ExitUnauthorized {
		t.Fatalf("log --instance stale: exit %d (%v); want %d\n%s", got, err, output.ExitUnauthorized, out)
	}
}

// startLocalInstance writes a live pidfile (this test process's pid) for
// a local dpkms named name on port.
func startLocalInstance(t *testing.T, name string, port int) {
	t.Helper()
	runDir, err := config.RunDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := pidfile.Write(runDir, pidfile.Info{PID: os.Getpid(), Name: name, Port: port}); err != nil {
		t.Fatal(err)
	}
}

// TestInstanceUse_NamedEndpointAndLocal: `instance use` accepts a named
// endpoint or a running local instance (by name or port, stored by name),
// rejects anything else with exit 70, and `-` clears the selection.
func TestInstanceUse_NamedEndpointAndLocal(t *testing.T) {
	db := setupTestDB(t)
	appendConfig(t, db, namedEndpoints("http://127.0.0.1:1", "https://home.example.net:7700"))
	startLocalInstance(t, "work", 18093)
	state, err := config.CurrentInstanceFile()
	if err != nil {
		t.Fatal(err)
	}
	read := func() string {
		b, _ := os.ReadFile(state)
		return string(b)
	}

	for _, step := range []struct{ sel, want string }{
		{"home", "home"}, {"work", "work"}, {"18093", "work"},
	} {
		if out, err := db.exec("instance", "use", step.sel); err != nil {
			t.Fatalf("instance use %s: %v\n%s", step.sel, err, out)
		}
		if got := read(); got != step.want {
			t.Errorf("after `instance use %s` state = %q; want %q", step.sel, got, step.want)
		}
	}

	out, err := db.exec("instance", "use", "nope")
	if got := ExitCodeFor(err); got != output.ExitPrerequisite {
		t.Fatalf("instance use nope: exit %d (%v); want %d\n%s", got, err, output.ExitPrerequisite, out)
	}
	if got := read(); got != "work" {
		t.Errorf("a rejected selection changed the state to %q", got)
	}

	if out, err := db.exec("instance", "use", "-"); err != nil {
		t.Fatalf("instance use -: %v\n%s", err, out)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Errorf("state file survives `instance use -`: %v", err)
	}
}

// TestInstanceList_NamedAndLocal: `instance list` shows each named
// server.urls entry and each running local instance, marks the one the
// resolver picks, and never prints a token.
func TestInstanceList_NamedAndLocal(t *testing.T) {
	db := setupTestDB(t)
	appendConfig(t, db, namedEndpoints("http://127.0.0.1:1", "https://home.example.net:7700"))
	startLocalInstance(t, "work", 18093)

	out, err := db.exec("instance", "list", "--instance", "home")
	if err != nil {
		t.Fatalf("instance list: %v\n%s", err, out)
	}
	for _, want := range []string{"decoy", "home", "https://home.example.net:7700", "work", "http://127.0.0.1:18093"} {
		if !strings.Contains(out, want) {
			t.Errorf("instance list lacks %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "home.example.net") && !strings.Contains(line, "*") {
			t.Errorf("the selected entry is not marked: %q", line)
		}
	}
	assertNoToken(t, out)

	out, err = db.exec("instance", "list", "--format", "json")
	if err != nil {
		t.Fatalf("instance list --format json: %v\n%s", err, out)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("instance list JSON: %v\n%s", err, out)
	}
	if len(rows) != 3 {
		t.Fatalf("instance list JSON has %d rows; want 3:\n%s", len(rows), out)
	}
	assertNoToken(t, out)
}

// TestInstanceCurrent_ReportsResolution: `instance current` prints the
// resolved URL, the layer that chose it and whether a token is attached,
// never the token.
func TestInstanceCurrent_ReportsResolution(t *testing.T) {
	db := setupTestDB(t)
	appendConfig(t, db, namedEndpoints("http://127.0.0.1:1", "https://home.example.net:7700"))

	out, err := db.exec("instance", "current", "--instance", "home")
	if err != nil {
		t.Fatalf("instance current: %v\n%s", err, out)
	}
	for _, want := range []string{"home", "https://home.example.net:7700", "--instance", "token: yes"} {
		if !strings.Contains(out, want) {
			t.Errorf("instance current lacks %q:\n%s", want, out)
		}
	}
	assertNoToken(t, out)

	writeCurrentInstance(t, "home")
	out, err = db.exec("instance", "current", "--format", "json")
	if err != nil {
		t.Fatalf("instance current --format json: %v\n%s", err, out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("instance current JSON: %v\n%s", err, out)
	}
	want := map[string]any{
		"name": "home", "url": "https://home.example.net:7700", "key": "home",
		"layer": string(dpkmsclient.LayerCurrent), "token": true, "local": false,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("instance current JSON %s = %v; want %v\n%s", k, got[k], v, out)
		}
	}
	assertNoToken(t, out)
}

// assertNoToken fails when out carries any configured token.
func assertNoToken(t *testing.T, out string) {
	t.Helper()
	for _, tok := range []string{"tok-home", "tok-default"} {
		if strings.Contains(out, tok) {
			t.Fatalf("output leaks token %q:\n%s", tok, out)
		}
	}
}

// asEnvelope reports whether err carries a kit envelope, storing it.
func asEnvelope(err error, e **output.Error) bool {
	return errors.As(err, e)
}
