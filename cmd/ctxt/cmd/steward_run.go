package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	kitcli "hop.top/kit/go/console/cli"
	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/cli"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
)

// stewardGrantReply is a steward grant as /api/v1/steward/grants
// answers it.
type stewardGrantReply struct {
	ID                 string          `json:"id"`
	RequestID          string          `json:"request_id"`
	StewardID          string          `json:"steward_id"`
	MatterRef          string          `json:"matter_ref,omitempty"`
	Scope              json.RawMessage `json:"scope"`
	ScopeDigest        string          `json:"scope_digest"`
	Status             string          `json:"status"`
	NotBefore          time.Time       `json:"not_before"`
	ExpiresAt          time.Time       `json:"expires_at"`
	ApprovedAt         time.Time       `json:"approved_at"`
	RevokedAt          *time.Time      `json:"revoked_at,omitempty"`
	RevokedBy          string          `json:"revoked_by,omitempty"`
	RevokeReason       string          `json:"revoke_reason,omitempty"`
	RenewsID           string          `json:"renews_id,omitempty"`
	State              string          `json:"state"`
	EffectiveExpiresAt time.Time       `json:"effective_expires_at"`
	RequestedBy        string          `json:"requested_by"`
}

// stewardPath is /api/v1/steward/grants, then the escaped segments.
func stewardPath(segments ...string) string {
	path := "/api/v1/steward/grants"
	for _, s := range segments {
		path += "/" + url.PathEscape(strings.TrimSpace(s))
	}
	return path
}

// stewardWhen parses a --not-before or --expires value against now.
func stewardWhen(flag, value string, now time.Time) (time.Time, error) {
	at, err := cli.ParseExpiry(value, now)
	if err != nil {
		return time.Time{}, output.UsageError(fmt.Sprintf("--%s: %v", flag, err))
	}
	return at, nil
}

// readStewardScope reads the scope document at path ("-" is stdin), YAML
// or JSON, and returns it as a JSON object.
func readStewardScope(cmd *cobra.Command, path string) (json.RawMessage, error) {
	var (
		data []byte
		err  error
	)
	if path == "-" {
		data, err = io.ReadAll(cmd.InOrStdin())
	} else {
		data, err = os.ReadFile(path) // #nosec G304 -- the operator names the scope file
	}
	if err != nil {
		return nil, output.UsageError(fmt.Sprintf("read --scope: %v", err))
	}
	var doc any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, output.UsageError(fmt.Sprintf("--scope: not YAML or JSON: %v", err))
	}
	if _, ok := doc.(map[string]any); !ok {
		return nil, output.UsageError("--scope: the document must be a share policy mapping (data, fields, capabilities, ...)")
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, output.UsageError(fmt.Sprintf("--scope: %v", err))
	}
	return raw, nil
}

// stewardRequestBody builds the filing body from the request flags.
func stewardRequestBody(cmd *cobra.Command) (map[string]any, error) {
	get := func(name string) string {
		v, _ := cmd.Flags().GetString(name)
		return strings.TrimSpace(v)
	}
	scope, audience := get("scope"), get("as-audience")
	if (scope == "") == (audience == "") {
		return nil, output.UsageError("give what the steward may read with --scope or --as-audience, one of them")
	}
	now := time.Now().UTC()
	expires, err := stewardWhen("expires", get("expires"), now)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"reason": get("reason"), "expires_at": expires.Format(time.RFC3339)}
	if nb := get("not-before"); nb != "" {
		at, err := stewardWhen("not-before", nb, now)
		if err != nil {
			return nil, err
		}
		body["not_before"] = at.Format(time.RFC3339)
	}
	for flag, key := range map[string]string{"steward": "steward_id", "matter": "matter_ref", "renews": "renews_id", "as-audience": "as_audience"} {
		if v := get(flag); v != "" {
			body[key] = v
		}
	}
	if scope != "" {
		raw, err := readStewardScope(cmd, scope)
		if err != nil {
			return nil, err
		}
		body["scope"] = raw
	}
	return body, nil
}

func runStewardRequest(cmd *cobra.Command, _ []string) error {
	body, err := stewardRequestBody(cmd)
	if err != nil {
		return err
	}
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	if kitcli.IsDryRun(cmd) {
		who := "yourself"
		if s, ok := body["steward_id"].(string); ok {
			who = s
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Would request steward access for %s until %s on %s\n", who, body["expires_at"], client.URL())
		return nil
	}
	var a approvalReply
	if err := client.Post(cmdContext(cmd), stewardPath(), body, &a); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{"pending": true, "request": a})
	}
	fmt.Fprintln(cmd.OutOrStdout(), a.ID)
	w := cmd.ErrOrStderr()
	fmt.Fprintf(w, "Waiting for approval: %s, expires %s.\n", a.ID, fmtStamp(a.ExpiresAt))
	fmt.Fprintln(w, "An approver other than you and the steward decides it with 'ctxt approval approve --digest <digest>'.")
	fmt.Fprintf(w, "See it with: ctxt approval show %s\n", a.ID)
	return nil
}

// stewardRow is the row shape the tabular formats project from.
type stewardRow struct {
	ID          string `table:"ID"`
	Steward     string `table:"Steward"`
	State       string `table:"State"`
	Matter      string `table:"Matter"`
	Expires     string `table:"Expires"`
	RequestedBy string `table:"Requested by"`
}

func renderStewardGrants(cmd *cobra.Command, w io.Writer, list []stewardGrantReply) error {
	if isJSONOutput() {
		return outputJSON(w, map[string]any{"grants": list, "total": len(list)})
	}
	rows := make([]stewardRow, 0, len(list))
	for _, g := range list {
		rows = append(rows, stewardRow{
			ID: g.ID, Steward: g.StewardID, State: g.State, Matter: g.MatterRef,
			Expires: fmtStamp(g.EffectiveExpiresAt), RequestedBy: g.RequestedBy,
		})
	}
	if cliformat.Rows() {
		return cliformat.DispatchRows(cmd, w, rows)
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "No steward grants.")
		return nil
	}
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		table = append(table, []string{r.ID, r.Steward, r.State, r.Matter, r.Expires, r.RequestedBy})
	}
	printTable(w, []string{"ID", "STEWARD", "STATE", "MATTER", "EXPIRES", "REQUESTED BY"}, table)
	return nil
}

func runStewardList(cmd *cobra.Command, _ []string) error {
	q, err := listQuery(cmd, "state", "steward")
	if err != nil {
		return err
	}
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	var reply apiList[stewardGrantReply]
	if err := client.Get(cmdContext(cmd), stewardPath(), q, &reply); err != nil {
		return err
	}
	return renderStewardGrants(cmd, cmd.OutOrStdout(), reply.Data)
}

func runStewardShow(cmd *cobra.Command, args []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	var g stewardGrantReply
	if err := client.Get(cmdContext(cmd), stewardPath(args[0]), nil, &g); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), g)
	}
	printStewardGrant(cmd.OutOrStdout(), g)
	return nil
}

// printStewardGrant writes a grant for a terminal.
func printStewardGrant(w io.Writer, g stewardGrantReply) {
	fmt.Fprintf(w, "%s  %s\n", g.ID, g.State)
	fmt.Fprintf(w, "Steward: %s, requested by %s (request %s)\n", g.StewardID, g.RequestedBy, g.RequestID)
	if g.MatterRef != "" {
		fmt.Fprintf(w, "Matter: %s\n", g.MatterRef)
	}
	fmt.Fprintf(w, "Window: %s to %s\n", fmtStamp(g.NotBefore), fmtStamp(g.EffectiveExpiresAt))
	if !g.EffectiveExpiresAt.Equal(g.ExpiresAt) {
		fmt.Fprintf(w, "  (approved until %s; steward.max_duration ends it sooner)\n", fmtStamp(g.ExpiresAt))
	}
	fmt.Fprintf(w, "Approved: %s\n", fmtStamp(g.ApprovedAt))
	if g.RevokedAt != nil {
		line := fmt.Sprintf("Revoked: %s by %s", fmtStamp(*g.RevokedAt), g.RevokedBy)
		if g.RevokeReason != "" {
			line += ": " + g.RevokeReason
		}
		fmt.Fprintln(w, line)
	}
	if g.RenewsID != "" {
		fmt.Fprintf(w, "Renews: %s\n", g.RenewsID)
	}
	fmt.Fprintf(w, "Scope digest: %s\n", g.ScopeDigest)
	fmt.Fprintf(w, "Scope: %s\n", string(g.Scope))
}

func runStewardRevoke(cmd *cobra.Command, args []string) error {
	reason, _ := cmd.Flags().GetString("reason")
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(args[0])
	path := stewardPath(id, "revoke")
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would revoke steward grant %s through %s on %s\n", id, path, client.URL())
		return nil
	}
	var g stewardGrantReply
	if err := client.Post(cmdContext(cmd), path, map[string]string{"reason": strings.TrimSpace(reason)}, &g); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), g)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Revoked steward grant %s.\n", g.ID)
	return nil
}
