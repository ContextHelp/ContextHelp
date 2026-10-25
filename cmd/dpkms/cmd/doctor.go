package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"hop.top/kit/go/core/uxp"

	"github.com/ideacrafterslabs/ctxt/internal/accessmarker"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/config"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check the instance's access posture on this host",
	Long: `Check the configured instance's access posture without opening its
database.

  access       the class in server.access against the access marker
               beside the database (<storage.path>.access)
  permissions  on a protected or public instance, the database, its WAL
               and shared-memory files, the lock and access sidecars and
               the local blob directory must not be readable by other
               users (0600 files, 0700 directories; run the daemon as a
               dedicated user). Not checked on Windows, whose access is an
               ACL.

A finding is a warning; with --strict a permissions finding fails the
command.

Examples:
  dpkms doctor
  dpkms doctor --strict --format json`,
	Args: cobra.NoArgs,
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
	doctorCmd.Flags().Bool("strict", false, "fail on a permissions finding instead of warning")
	cliconv.WithSideEffect(doctorCmd, cliconv.SideEffectRead)
}

// doctorCheck is one rendered check.
type doctorCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

var doctorStatusNames = map[uxp.CheckStatus]string{
	uxp.StatusOK: "ok", uxp.StatusWarn: "warn", uxp.StatusFail: "fail", uxp.StatusSkip: "skip",
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	strict, _ := cmd.Flags().GetBool("strict")
	d := uxp.NewDoctor()
	d.Add(func() uxp.Check { return accessCheck(cfg) })
	d.Add(func() uxp.Check { return permissionsCheck(cfg, strict) })
	results := d.Run()

	checks := make([]doctorCheck, len(results))
	failed := 0
	for i, r := range results {
		checks[i] = doctorCheck{Name: r.Name, Status: doctorStatusNames[r.Status], Message: r.Message, Detail: r.Detail}
		if r.Status == uxp.StatusFail {
			failed++
		}
	}
	if isJSONOutput() {
		if err := outputJSON(cmd.OutOrStdout(), map[string]any{"checks": checks}); err != nil {
			return err
		}
	} else {
		w := cmd.OutOrStdout()
		for _, c := range checks {
			_, _ = fmt.Fprintf(w, "%-5s %-12s %s\n", c.Status, c.Name, c.Message)
			if c.Detail != "" {
				for _, line := range strings.Split(c.Detail, "\n") {
					_, _ = fmt.Fprintf(w, "      %s\n", line)
				}
			}
		}
	}
	if failed > 0 {
		return fmt.Errorf("doctor: %d check(s) failed", failed)
	}
	return nil
}

// effectiveClass is the stricter of the configured class and the
// recorded one: a file left protected is guarded as protected.
func effectiveClass(configured string, m accessmarker.Marker) string {
	if !m.Private() {
		return m.Access
	}
	return configured
}

func accessCheck(c *config.Config) uxp.Check {
	ck := uxp.Check{Name: "access"}
	dbPath, ok := markerDBPath(c)
	configured := c.Server.EffectiveAccess()
	if !ok {
		ck.Status, ck.Message = uxp.StatusSkip, "no database file (postgres or in-memory): no access marker"
		return ck
	}
	m, found, err := accessmarker.Read(dbPath)
	switch {
	case err != nil:
		ck.Status, ck.Message, ck.Detail = uxp.StatusWarn, "access marker unreadable: direct opens are refused", err.Error()
	case configured != config.AccessPrivate && (!found || m.Access != configured):
		ck.Status = uxp.StatusWarn
		ck.Message = fmt.Sprintf("server.access is %s but the marker records %s: start dpkms serve to record it", configured, m.Access)
	case configured == config.AccessPrivate && !m.Private():
		ck.Status = uxp.StatusWarn
		ck.Message = fmt.Sprintf("server.access is private but the marker records %s: direct opens stay refused until `dpkms instance set-access private`", m.Access)
	default:
		ck.Status, ck.Message = uxp.StatusOK, "access "+m.Access
	}
	return ck
}

func permissionsCheck(c *config.Config, strict bool) uxp.Check {
	ck := uxp.Check{Name: "permissions"}
	dbPath, ok := markerDBPath(c)
	if !ok {
		ck.Status, ck.Message = uxp.StatusSkip, "no database file"
		return ck
	}
	m, _, err := accessmarker.Read(dbPath)
	if err != nil {
		m = accessmarker.Marker{Access: config.AccessProtected} // fail closed: check as guarded
	}
	class := effectiveClass(c.Server.EffectiveAccess(), m)
	if class == config.AccessPrivate {
		ck.Status, ck.Message = uxp.StatusSkip, "private instance: file modes not checked"
		return ck
	}
	findings, err := accessmarker.CheckPermissions(dbPath, blobDirOf(c))
	if errors.Is(err, accessmarker.ErrModesUnsupported) {
		ck.Status, ck.Message = uxp.StatusSkip, err.Error()
		return ck
	}
	if err != nil {
		ck.Status, ck.Message = uxp.StatusFail, err.Error()
		return ck
	}
	if len(findings) == 0 {
		ck.Status, ck.Message = uxp.StatusOK, class+" instance: files closed to other users"
		return ck
	}
	ck.Status = uxp.StatusWarn
	if strict {
		ck.Status = uxp.StatusFail
	}
	ck.Message = fmt.Sprintf("%s instance: %d file(s) readable by other users; run the daemon as a dedicated user and chmod them", class, len(findings))
	ck.Detail = strings.Join(permissionFixLines(findings, blobDirOf(c)), "\n")
	return ck
}

// maxListedFindings caps the per-file lines doctor and serve print.
const maxListedFindings = 10

// permissionFixLines renders findings with the command that fixes each,
// at most maxListedFindings of them, and one command for the whole blob
// directory when any finding is under it (old blobs keep the modes an
// earlier store gave them).
func permissionFixLines(findings []accessmarker.Finding, blobDir string) []string {
	var lines []string
	inBlobs := false
	for i, f := range findings {
		if blobDir != "" && strings.HasPrefix(f.Path, blobDir) {
			inBlobs = true
		}
		if i < maxListedFindings {
			lines = append(lines, f.String(), "  fix: "+f.Fix())
		}
	}
	if n := len(findings) - maxListedFindings; n > 0 {
		lines = append(lines, fmt.Sprintf("... and %d more", n))
	}
	if inBlobs {
		lines = append(lines, "fix the whole blob directory (as the database owner): "+accessmarker.BlobTreeFix(blobDir))
	}
	return lines
}
