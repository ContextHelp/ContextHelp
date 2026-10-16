package cmd

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// Credentials a configured locator embeds. Each is at least 16
// characters (redaction keeps the last 4) and shares no 4-character
// run with another.
const (
	hcURLPass  = "Rv7NcQ2wLx9pKd4m0201"
	hcURLQuery = "Ty3HbZ8fWq1sMg6k0202"
	psDBPass   = "Ux5JeA9rVn2tCy7h0203"
)

// assertNoURLSecret fails when s shows any 4-character run of a secret
// other than the last 4 characters redaction keeps.
func assertNoURLSecret(t *testing.T, label, s string) {
	t.Helper()
	for _, sec := range []string{hcURLPass, hcURLQuery, psDBPass} {
		hidden := sec[:len(sec)-4]
		for i := 0; i+4 <= len(hidden); i++ {
			if strings.Contains(s, hidden[i:i+4]) {
				t.Errorf("%s leaks %q (shows %q):\n%s", label, sec, hidden[i:i+4], s)
				break
			}
		}
	}
}

// dpkms healthcheck names the server it checked, and dpkms ps each
// instance's database: a password or token the URL embeds stays masked
// in every --format and in the error of an unreachable server.
func TestHealthcheckAndPsRedactURLs(t *testing.T) {
	srv := fakeDpkmsHealthzServer(t, http.StatusOK, HealthzEnvelope{
		Health: "healthy", Checks: HealthzChecks{Watchers: []HealthzWatcher{}},
	})
	t.Cleanup(srv.Close)
	// Other healthcheck tests point the command's writers at their own
	// buffers; write through the root's, which executeCommand captures.
	healthcheckCmd.SetOut(nil)
	healthcheckCmd.SetErr(nil)

	dir := t.TempDir()
	t.Setenv(config.EnvDataDir, dir)
	runDir, err := config.RunDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := pidfile.Write(runDir, pidfile.Info{
		PID: os.Getpid(), Name: "pg", Port: 47616, StartedAt: time.Now(),
		DBPath: "postgres://pguser:" + psDBPass + "@db.internal:5432/ctxt?sslmode=disable",
	}); err != nil {
		t.Fatal(err)
	}

	commands := []struct {
		args  []string
		shown string // the masked form the default format prints
	}{
		{[]string{"healthcheck", "--server-url", strings.Replace(srv.URL, "://", "://hc:"+hcURLPass+"@", 1)}, "hc:****0201@"},
		{[]string{"healthcheck", "--server-url", testguard.ClosedServerURL + "/?token=" + hcURLQuery}, "token=****0202"},
		{[]string{"ps"}, "pguser:****0203@"},
	}
	formats := append([]string{""}, cliformat.Valid()...)
	for _, c := range commands {
		for _, f := range formats {
			name := f
			if name == "" {
				name = "default"
			}
			t.Run(c.args[0]+"/"+name, func(t *testing.T) {
				args := append([]string{}, c.args...)
				if f != "" {
					args = append(args, "--format", f)
				}
				out, err := executeCommand(args...)
				all := out
				if err != nil {
					all += "\n" + err.Error()
				}
				assertNoURLSecret(t, "output", all)
				if f == "" && !strings.Contains(all, c.shown) {
					t.Errorf("want masked %q in output:\n%s", c.shown, all)
				}
			})
		}
	}
}
