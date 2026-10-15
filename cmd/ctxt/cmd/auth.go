package cmd

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/cli"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ctxt auth: the caller's own identity, API keys, identities and
// browser sessions on the dpkms instance, over the API (ADR-079 §5).
// Own credentials go through /api/v1/self/* under write:self, which
// every role holds; --principal on the key commands switches to
// /api/v1/principals/{id}/keys under admin:principals.

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Your identity, API keys and sessions on the dpkms instance",
	Long: `Manage your own credentials on the dpkms instance ctxt talks to.

Every command calls the instance (--server, --instance, CTXT_INSTANCE,
the current instance, the first server.urls entry, or server.url) with
its configured API key. What you may manage is your own: the keys,
identities and browser sessions of the principal your key acts for.
An admin key manages other principals' keys with --principal on the
key commands, and the rest of a principal with 'ctxt principal'.

Examples:
  # Who the configured key acts for, with its scopes
  ctxt auth whoami

  # A second key for yourself, expiring in 30 days
  ctxt auth key create --name "assistant bot" --expires 30d

  # End every browser session but the keys
  ctxt auth session list`,
}

var authWhoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show the principal the configured key acts for",
	Long: `Show who the instance takes the configured key for: the principal, its
roles, the effective scopes and how it authenticated.

Examples:
  ctxt auth whoami
  ctxt auth whoami --server https://dpkms.example.net`,
	Args: cobra.NoArgs,
	RunE: runAuthWhoami,
}

var authKeyCmd = &cobra.Command{
	Use:   "key",
	Short: "Create, list and revoke API keys",
	Long: `Create, list and revoke API keys.

Without --principal the commands manage your own keys. With --principal
they manage another principal's, which takes an admin key
(admin:principals). A key never holds more than the principal's roles
grant, nor more than the key that created it holds.`,
}

var authKeyCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an API key and print it once",
	Long: `Create an API key and print it once.

The key goes to stdout alone, so it can be captured; a one-line
confirmation goes to stderr. Only its hash is stored: a lost key is
revoked and replaced, never recovered.

The key holds your key's scopes, or fewer with --scope. It never expires
unless --expires gives a duration (720h, 30d) or a date (2026-12-31, or
RFC 3339; a date expires at the end of that day, UTC). With --principal
an admin creates a key for another principal, holding that principal's
role scopes unless --scope narrows them: this is how a federation peer
or a capture device gets its key.

Examples:
  # A key for an agent, reads only, gone in a week
  KEY=$(ctxt auth key create --name "assistant" --scope read:objects --scope read:mcp --expires 7d)

  # A federation peer's key (the peer principal exists already)
  ctxt auth key create --principal prn_01J... --name "partner push"`,
	Args: cobra.NoArgs,
	RunE: runAuthKeyCreate,
}

var authKeyListCmd = &cobra.Command{
	Use:   "list",
	Short: "List API keys, revoked ones included",
	Long: `List your API keys, or another principal's with --principal (admin).
Revoked and expired keys stay listed with their dates.

Examples:
  ctxt auth key list
  ctxt auth key list --principal prn_01J... --format json`,
	Args: cobra.NoArgs,
	RunE: runAuthKeyList,
}

var authKeyRevokeCmd = &cobra.Command{
	Use:   "revoke <key-id>",
	Short: "Revoke an API key",
	Long: `Revoke an API key by id (the middle part of the key, as 'ctxt auth key
list' shows it). The key stops working at once, and every browser
session it signed in ends with it. Your own keys only, unless
--principal names the principal the key belongs to (admin).

Examples:
  ctxt auth key revoke 3f2a9c1e0b7d4e66
  ctxt auth key revoke 3f2a9c1e0b7d4e66 --principal prn_01J...`,
	Args: cobra.ExactArgs(1),
	RunE: runAuthKeyRevoke,
}

var authIdentityCmd = &cobra.Command{
	Use:   "identity",
	Short: "List and remove your own identities",
	Long: `List and remove the identities bound to your principal: API keys, the
links you signed in with, proxy logins and OIDC subjects.`,
}

var authIdentityListCmd = &cobra.Command{
	Use:   "list",
	Short: "List your identities",
	Long: `List every identity bound to your principal, revoked ones included.

Examples:
  ctxt auth identity list`,
	Args: cobra.NoArgs,
	RunE: runAuthIdentityList,
}

var authIdentityRemoveCmd = &cobra.Command{
	Use:   "remove <identity-id>",
	Short: "Remove one of your identities",
	Long: `Remove an identity of yours by id (idn_...). The credential behind it
stops working at once, with every browser session it signed in: an API
key's identity takes the key with it, a link's identity ends the session
that link opened.

Examples:
  ctxt auth identity remove idn_01J...`,
	Args: cobra.ExactArgs(1),
	RunE: runAuthIdentityRemove,
}

var authSessionCmd = &cobra.Command{
	Use:   "session",
	Short: "List and end your browser sessions",
	Long:  `List and end the web UI browser sessions of your principal.`,
}

var authSessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List your browser sessions",
	Long: `List your active web UI sessions, newest first; --all adds the ended
ones.

Examples:
  ctxt auth session list
  ctxt auth session list --all --format json`,
	Args: cobra.NoArgs,
	RunE: runAuthSessionList,
}

var authSessionEndCmd = &cobra.Command{
	Use:   "end <session-id>",
	Short: "End one of your browser sessions",
	Long: `End one of your web UI sessions by id (uis_...). The browser is signed
out on its next request. Another principal's session is refused; an
operator ends those with 'dpkms session revoke' on the host.

Examples:
  ctxt auth session end uis_3f2a...`,
	Args: cobra.ExactArgs(1),
	RunE: runAuthSessionEnd,
}

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.AddCommand(authWhoamiCmd, authKeyCmd, authIdentityCmd, authSessionCmd)
	authKeyCmd.AddCommand(authKeyCreateCmd, authKeyListCmd, authKeyRevokeCmd)
	authIdentityCmd.AddCommand(authIdentityListCmd, authIdentityRemoveCmd)
	authSessionCmd.AddCommand(authSessionListCmd, authSessionEndCmd)

	leaves := []*cobra.Command{
		authWhoamiCmd, authKeyCreateCmd, authKeyListCmd, authKeyRevokeCmd,
		authIdentityListCmd, authIdentityRemoveCmd, authSessionListCmd, authSessionEndCmd,
	}
	for _, c := range leaves {
		c.Flags().String("server", "", serverFlagUsage)
	}
	for _, c := range []*cobra.Command{authKeyCreateCmd, authKeyListCmd, authKeyRevokeCmd} {
		c.Flags().String("principal", "", "manage this principal's keys instead of your own (admin)")
	}
	authKeyCreateCmd.Flags().String("name", "", "label for the key (\"laptop CLI\")")
	authKeyCreateCmd.Flags().String("expires", "", "expiry as a duration (720h, 30d) or a date (2026-12-31); never by default")
	authKeyCreateCmd.Flags().StringSlice("scope", nil, "narrow the key to these scopes (repeatable); default: every scope it may hold")
	authSessionListCmd.Flags().Bool("all", false, "include ended (expired and revoked) sessions")

	cliconv.WithSideEffect(authWhoamiCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(authWhoamiCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(authWhoamiCmd, []cliconv.Example{
		{Title: "Who the configured key acts for", Command: "ctxt auth whoami"},
		{Title: "As JSON", Command: "ctxt auth whoami --format json"},
	})

	// Creating a key adds a credential; every run mints a different one.
	cliconv.WithSideEffect(authKeyCreateCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(authKeyCreateCmd, cliconv.IdempotencyNo)
	cliconv.WithExamples(authKeyCreateCmd, []cliconv.Example{
		{Title: "A key for yourself", Command: "ctxt auth key create --name \"laptop CLI\""},
		{Title: "A narrow, short-lived key for an agent", Command: "ctxt auth key create --name assistant --scope read:objects --expires 7d"},
		{Title: "A key for a federation peer", Command: "ctxt auth key create --principal prn_01J... --name \"partner push\""},
	})
	cliconv.WithNextSteps(authKeyCreateCmd, []cliconv.NextStep{
		{When: "to use it from ctxt", Suggest: "ctxt config edit", Reason: "put the key in server.token, or in the token of a server.urls entry"},
		{When: "to see every key", Suggest: "ctxt auth key list", Reason: "ids, scopes, expiry and last use"},
	})

	cliconv.WithSideEffect(authKeyListCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(authKeyListCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(authKeyListCmd, []cliconv.Example{
		{Title: "Your keys", Command: "ctxt auth key list"},
		{Title: "Another principal's keys", Command: "ctxt auth key list --principal prn_01J..."},
	})

	// Revoking ends a credential for good; a revoked key stays revoked.
	cliconv.WithSideEffect(authKeyRevokeCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(authKeyRevokeCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(authKeyRevokeCmd, []cliconv.Example{
		{Title: "Revoke one of your keys", Command: "ctxt auth key revoke 3f2a9c1e0b7d4e66"},
	})
	cliconv.WithNextSteps(authKeyRevokeCmd, []cliconv.NextStep{
		{When: "to replace it", Suggest: "ctxt auth key create", Reason: "a revoked key is never recovered"},
	})

	cliconv.WithSideEffect(authIdentityListCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(authIdentityListCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(authIdentityListCmd, []cliconv.Example{
		{Title: "Your identities", Command: "ctxt auth identity list"},
	})
	cliconv.WithSideEffect(authIdentityRemoveCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(authIdentityRemoveCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(authIdentityRemoveCmd, []cliconv.Example{
		{Title: "Remove an identity", Command: "ctxt auth identity remove idn_01J..."},
	})
	cliconv.WithNextSteps(authIdentityRemoveCmd, []cliconv.NextStep{
		{When: "after removing", Suggest: "ctxt auth identity list", Reason: "the row stays listed as revoked"},
	})

	cliconv.WithSideEffect(authSessionListCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(authSessionListCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(authSessionListCmd, []cliconv.Example{
		{Title: "Your active sessions", Command: "ctxt auth session list"},
		{Title: "Ended ones too", Command: "ctxt auth session list --all"},
	})
	cliconv.WithSideEffect(authSessionEndCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(authSessionEndCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(authSessionEndCmd, []cliconv.Example{
		{Title: "Sign one browser out", Command: "ctxt auth session end uis_3f2a..."},
	})
	cliconv.WithNextSteps(authSessionEndCmd, []cliconv.NextStep{
		{When: "to sign that browser in again", Suggest: "ctxt ui open", Reason: "each sign-in is a new session"},
	})
}

// apiList is the envelope of every list the registry routes answer.
type apiList[T any] struct {
	Data  []T `json:"data"`
	Total int `json:"total"`
}

// createdKeyReply is the body of a key creation: the key, shown once.
type createdKeyReply struct {
	Key string `json:"key"`
	authn.IssuedKey
}

// whoamiReply is the body of GET /api/v1/whoami.
type whoamiReply struct {
	Principal string        `json:"principal"`
	Name      string        `json:"name,omitempty"`
	Provider  string        `json:"provider"`
	Roles     []string      `json:"roles"`
	Scopes    []authn.Scope `json:"scopes"`
	Via       string        `json:"via"`
}

// cmdContext returns the command's context, or a background one.
func cmdContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// keysPath is where the key commands go: the caller's own keys, or a
// principal's under admin:principals.
func keysPath(cmd *cobra.Command) string {
	if p, _ := cmd.Flags().GetString("principal"); strings.TrimSpace(p) != "" {
		return "/api/v1/principals/" + url.PathEscape(strings.TrimSpace(p)) + "/keys"
	}
	return "/api/v1/self/keys"
}

// fmtStamp renders a time for a table, "" for the zero time.
func fmtStamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}

func fmtStampPtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return fmtStamp(*t)
}

func runAuthWhoami(cmd *cobra.Command, _ []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	var who whoamiReply
	if err := client.Get(cmdContext(cmd), "/api/v1/whoami", nil, &who); err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if isJSONOutput() {
		return outputJSON(out, who)
	}
	fmt.Fprintf(out, "Principal: %s", who.Principal)
	if who.Name != "" && who.Name != who.Principal {
		fmt.Fprintf(out, " (%s)", who.Name)
	}
	fmt.Fprintf(out, "\nRoles:     %s\nVia:       %s (%s)\nScopes:    %s\n",
		strings.Join(who.Roles, ", "), who.Via, who.Provider, joinScopes(who.Scopes))
	return nil
}

func joinScopes(scopes []authn.Scope) string {
	names := make([]string, len(scopes))
	for i, s := range scopes {
		names[i] = string(s)
	}
	return strings.Join(names, ", ")
}

func runAuthKeyCreate(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	expires, _ := cmd.Flags().GetString("expires")
	scopes, _ := cmd.Flags().GetStringSlice("scope")
	expiresAt, err := cli.ParseExpiry(expires, time.Now().UTC())
	if err != nil {
		return err
	}
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	path := keysPath(cmd)
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would create an API key through %s on %s\n", path, client.URL())
		return nil
	}
	body := map[string]any{"name": name}
	if !expiresAt.IsZero() {
		body["expires_at"] = expiresAt
	}
	if len(scopes) > 0 {
		body["scopes"] = scopes
	}
	var reply createdKeyReply
	if err := client.Post(cmdContext(cmd), path, body, &reply); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), reply)
	}
	fmt.Fprintln(cmd.OutOrStdout(), reply.Key)
	var detail strings.Builder
	if reply.Name != "" {
		fmt.Fprintf(&detail, " (%s)", reply.Name)
	}
	if reply.ExpiresAt.IsZero() {
		detail.WriteString(", no expiry")
	} else {
		fmt.Fprintf(&detail, ", expires %s", reply.ExpiresAt.UTC().Format(time.RFC3339))
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Created API key %s for principal %s%s. Shown once; only its hash is stored.\n",
		reply.ID, reply.PrincipalID, detail.String())
	return nil
}

// keyRow is the row shape the tabular formats project from.
type keyRow struct {
	ID       string `table:"ID"`
	Name     string `table:"Name"`
	Scopes   string `table:"Scopes"`
	Created  string `table:"Created"`
	Expires  string `table:"Expires"`
	LastUsed string `table:"Last used"`
	Status   string `table:"Status"`
}

func keyStatus(k authn.KeyInfo, now time.Time) string {
	switch {
	case k.RevokedAt != nil:
		return "revoked"
	case !k.ExpiresAt.IsZero() && !now.Before(k.ExpiresAt):
		return "expired"
	}
	return "active"
}

func runAuthKeyList(cmd *cobra.Command, _ []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	var reply apiList[authn.KeyInfo]
	if err := client.Get(cmdContext(cmd), keysPath(cmd), nil, &reply); err != nil {
		return err
	}
	return renderKeys(cmd, cmd.OutOrStdout(), reply.Data)
}

func renderKeys(cmd *cobra.Command, w io.Writer, keys []authn.KeyInfo) error {
	if isJSONOutput() {
		return outputJSON(w, map[string]any{"keys": keys, "total": len(keys)})
	}
	now := time.Now()
	rows := make([]keyRow, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, keyRow{
			ID: k.ID, Name: k.Name, Scopes: fmt.Sprintf("%d", len(k.Scopes)), Created: fmtStamp(k.CreatedAt),
			Expires: fmtStamp(k.ExpiresAt), LastUsed: fmtStampPtr(k.LastUsedAt), Status: keyStatus(k, now),
		})
	}
	if cliformat.Rows() {
		return cliformat.DispatchRows(cmd, w, rows)
	}
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		table = append(table, []string{r.ID, r.Name, r.Scopes, r.Created, r.Expires, r.LastUsed, r.Status})
	}
	printTable(w, []string{"ID", "NAME", "SCOPES", "CREATED", "EXPIRES", "LAST USED", "STATUS"}, table)
	return nil
}

func runAuthKeyRevoke(cmd *cobra.Command, args []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(args[0])
	path := keysPath(cmd) + "/" + url.PathEscape(id)
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would revoke API key %s through %s on %s\n", id, path, client.URL())
		return nil
	}
	if err := client.Do(cmdContext(cmd), dpkmsclient.Request{Method: "DELETE", Path: path}, nil); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{"id": id, "revoked": true})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Revoked API key %s.\n", id)
	return nil
}

// identityRow is the row shape the tabular formats project from.
type identityRow struct {
	ID       string `table:"ID"`
	Method   string `table:"Method"`
	Issuer   string `table:"Issuer"`
	Subject  string `table:"Subject"`
	Name     string `table:"Name"`
	Created  string `table:"Created"`
	LastUsed string `table:"Last used"`
	Status   string `table:"Status"`
}

func renderIdentities(cmd *cobra.Command, w io.Writer, idns []storage.Identity) error {
	if isJSONOutput() {
		return outputJSON(w, map[string]any{"identities": idns, "total": len(idns)})
	}
	rows := make([]identityRow, 0, len(idns))
	for _, i := range idns {
		status := "active"
		if i.RevokedAt != nil {
			status = "revoked"
		}
		subject := i.Subject
		if len(subject) > 24 {
			subject = subject[:21] + "..."
		}
		rows = append(rows, identityRow{
			ID: i.ID, Method: i.Method, Issuer: i.Issuer, Subject: subject, Name: i.Name,
			Created: fmtStamp(i.CreatedAt), LastUsed: fmtStampPtr(i.LastUsedAt), Status: status,
		})
	}
	if cliformat.Rows() {
		return cliformat.DispatchRows(cmd, w, rows)
	}
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		table = append(table, []string{r.ID, r.Method, r.Issuer, r.Subject, r.Name, r.Created, r.LastUsed, r.Status})
	}
	printTable(w, []string{"ID", "METHOD", "ISSUER", "SUBJECT", "NAME", "CREATED", "LAST USED", "STATUS"}, table)
	return nil
}

func runAuthIdentityList(cmd *cobra.Command, _ []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	var reply apiList[storage.Identity]
	if err := client.Get(cmdContext(cmd), "/api/v1/self/identities", nil, &reply); err != nil {
		return err
	}
	return renderIdentities(cmd, cmd.OutOrStdout(), reply.Data)
}

func runAuthIdentityRemove(cmd *cobra.Command, args []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(args[0])
	path := "/api/v1/self/identities/" + url.PathEscape(id)
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would remove identity %s through %s on %s\n", id, path, client.URL())
		return nil
	}
	if err := client.Do(cmdContext(cmd), dpkmsclient.Request{Method: "DELETE", Path: path}, nil); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{"id": id, "removed": true})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed identity %s.\n", id)
	return nil
}

// ownSessionReply is one listed session of the caller.
type ownSessionReply struct {
	storage.UISession
	Status  string `json:"status"`
	Current bool   `json:"current,omitempty"`
}

// ownSessionRow is the row shape the tabular formats project from.
type ownSessionRow struct {
	ID       string `table:"ID"`
	Status   string `table:"Status"`
	Created  string `table:"Created"`
	LastSeen string `table:"Last seen"`
	Expires  string `table:"Expires"`
	From     string `table:"From"`
	Browser  string `table:"Browser"`
}

func runAuthSessionList(cmd *cobra.Command, _ []string) error {
	all, _ := cmd.Flags().GetBool("all")
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	q := url.Values{}
	if all {
		q.Set("all", "1")
	}
	var reply apiList[ownSessionReply]
	if err := client.Get(cmdContext(cmd), "/api/v1/self/sessions", q, &reply); err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if isJSONOutput() {
		return outputJSON(out, map[string]any{"sessions": reply.Data, "total": len(reply.Data)})
	}
	rows := make([]ownSessionRow, 0, len(reply.Data))
	for _, s := range reply.Data {
		ua := s.UserAgent
		if len(ua) > 40 {
			ua = ua[:37] + "..."
		}
		rows = append(rows, ownSessionRow{
			ID: s.ID, Status: s.Status, Created: fmtStamp(s.CreatedAt), LastSeen: fmtStamp(s.LastSeenAt),
			Expires: fmtStamp(s.ExpiresAt), From: s.RemoteAddr, Browser: ua,
		})
	}
	if cliformat.Rows() {
		return cliformat.DispatchRows(cmd, out, rows)
	}
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		table = append(table, []string{r.ID, r.Status, r.Created, r.LastSeen, r.Expires, r.From, r.Browser})
	}
	printTable(out, []string{"ID", "STATUS", "CREATED", "LAST SEEN", "EXPIRES", "FROM", "BROWSER"}, table)
	return nil
}

func runAuthSessionEnd(cmd *cobra.Command, args []string) error {
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(args[0])
	path := "/api/v1/self/sessions/" + url.PathEscape(id)
	if kitcli.IsDryRun(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "Would end session %s through %s on %s\n", id, path, client.URL())
		return nil
	}
	if err := client.Do(cmdContext(cmd), dpkmsclient.Request{Method: "DELETE", Path: path}, nil); err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{"id": id, "ended": true})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Ended session %s.\n", id)
	return nil
}
