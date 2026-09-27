package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "List and revoke web UI browser sessions",
	Long: `List and revoke the browser sessions of the web UI.

A browser signs in to a protected or public instance with a link from
'ctxt ui open'. Each sign-in is a session stored in the database: it acts
as the principal of the API token that minted the link, ends after
server.ui.session.idle_ttl without a request (default 12h) or
server.ui.session.max_ttl after sign-in (default 168h), and ends at once
when that token is removed from server.auth.static.tokens.

These commands read the database directly; a running dpkms sees a
revocation on the session's next request.

Examples:
  # Active sessions
  dpkms session list

  # Every session, ended ones included, as JSON
  dpkms session list --all --format json

  # Sign one browser out
  dpkms session revoke uis_3f2a...

  # Sign a principal out everywhere
  dpkms session revoke --principal ops`,
}

var sessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List web UI sessions",
	Long: `List web UI browser sessions, newest first. Only active sessions are
shown unless --all is set.`,
	Args: cobra.NoArgs,
	RunE: runSessionList,
}

var sessionRevokeCmd = &cobra.Command{
	Use:   "revoke [session-id]",
	Short: "Revoke a web UI session",
	Long: `Revoke one web UI session by ID, or every active session of a
principal with --principal. The browser is signed out on its next
request; signing in again takes a new 'ctxt ui open' link.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSessionRevoke,
}

func init() {
	rootCmd.AddCommand(sessionCmd)
	sessionCmd.AddCommand(sessionListCmd)
	sessionCmd.AddCommand(sessionRevokeCmd)

	sessionListCmd.Flags().Bool("all", false, "include ended (expired and revoked) sessions")
	sessionListCmd.Flags().String("principal", "", "only this principal's sessions")
	sessionListCmd.Flags().Int("limit", 100, "maximum number of sessions to list")
	sessionRevokeCmd.Flags().String("principal", "", "revoke every active session of this principal")

	cliconv.WithSideEffect(sessionListCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(sessionListCmd, cliconv.IdempotencyYes)
	// Revoking ends a sign-in; the browser can sign in again with a new
	// link, and revoking a revoked session changes nothing. Write.
	cliconv.WithSideEffect(sessionRevokeCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(sessionRevokeCmd, cliconv.IdempotencyYes)
}

// sessionStatus is a session's state as a listing shows it.
func sessionStatus(s *storage.UISession, now time.Time, resolver authn.TokenHashResolver) string {
	switch {
	case s.RevokedAt != nil:
		if s.RevokeReason != "" {
			return "revoked (" + s.RevokeReason + ")"
		}
		return "revoked"
	case !s.Active(now):
		return "expired"
	case resolver != nil:
		if p, ok := resolver.PrincipalForTokenHash(s.TokenHash); !ok || p.ID != s.PrincipalID {
			return "token removed"
		}
	}
	return "active"
}

// sessionView is one listed session.
type sessionView struct {
	*storage.UISession
	Status string `json:"status"`
}

// sessionRow is the row shape the tabular formats project from.
type sessionRow struct {
	ID        string `table:"ID"`
	Principal string `table:"Principal"`
	Status    string `table:"Status"`
	LastSeen  string `table:"Last seen"`
	Expires   string `table:"Expires"`
	From      string `table:"From"`
	Browser   string `table:"Browser"`
}

// openSessionStore opens the configured database for the session
// commands. The caller closes the returned driver.
func openSessionStore(ctx context.Context) (storage.StorageDriver, error) {
	storageType := cfg.Storage.Type
	if storageType == "" {
		storageType = "sqlite"
	}
	if cfg.Storage.Path == "" {
		return nil, errors.New("storage path not configured")
	}
	driver, err := storageutil.NewDriver(storageType, cfg.Storage.Path)
	if err != nil {
		return nil, fmt.Errorf("init storage: %w", err)
	}
	if err := driver.Init(ctx); err != nil {
		_ = driver.Close(ctx)
		return nil, fmt.Errorf("init storage: %w", err)
	}
	return driver, nil
}

// configuredTokenResolver resolves token hashes against the configured
// static tokens, or returns nil when no such provider is configured.
func configuredTokenResolver() authn.TokenHashResolver {
	p, err := authn.FromConfig(cfg.Server.Auth)
	if err != nil || p == nil {
		return nil
	}
	r, _ := p.(authn.TokenHashResolver)
	return r
}

func runSessionList(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	all, _ := cmd.Flags().GetBool("all")
	principal, _ := cmd.Flags().GetString("principal")
	limit, _ := cmd.Flags().GetInt("limit")

	driver, err := openSessionStore(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = driver.Close(context.Background()) }()

	now := time.Now().UTC()
	sessions, err := driver.UISessions().List(ctx, storage.UISessionFilter{
		ActiveOnly: !all, Now: now, PrincipalID: principal, Limit: limit,
	})
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}
	resolver := configuredTokenResolver()
	views := make([]sessionView, 0, len(sessions))
	for _, s := range sessions {
		views = append(views, sessionView{UISession: s, Status: sessionStatus(s, now, resolver)})
	}
	return renderSessions(cmd, cmd.OutOrStdout(), views)
}

func renderSessions(cmd *cobra.Command, w io.Writer, views []sessionView) error {
	if isJSONOutput() {
		return outputJSON(w, map[string]any{"sessions": views, "total": len(views)})
	}
	rows := make([]sessionRow, 0, len(views))
	for _, v := range views {
		ua := v.UserAgent
		if len(ua) > 40 {
			ua = ua[:37] + "..."
		}
		rows = append(rows, sessionRow{
			ID:        v.ID,
			Principal: v.PrincipalID,
			Status:    v.Status,
			LastSeen:  v.LastSeenAt.Local().Format("2006-01-02 15:04"),
			Expires:   v.ExpiresAt.Local().Format("2006-01-02 15:04"),
			From:      v.RemoteAddr,
			Browser:   ua,
		})
	}
	if cliformat.Rows() {
		return cliformat.DispatchRows(cmd, w, rows)
	}
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		table = append(table, []string{r.ID, r.Principal, r.Status, r.LastSeen, r.Expires, r.From, r.Browser})
	}
	printAdminTable(w, []string{"ID", "Principal", "Status", "Last seen", "Expires", "From", "Browser"}, table)
	return nil
}

func runSessionRevoke(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	principal, _ := cmd.Flags().GetString("principal")
	switch {
	case len(args) == 1 && principal != "":
		return errors.New("give a session ID or --principal, not both")
	case len(args) == 0 && principal == "":
		return errors.New("give a session ID, or --principal to revoke every session of a principal")
	}

	driver, err := openSessionStore(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = driver.Close(context.Background()) }()
	store := driver.UISessions()
	now := time.Now().UTC()

	var revoked []string
	if len(args) == 1 {
		if err := store.Revoke(ctx, args[0], now, authn.RevokeReasonRevoked); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return fmt.Errorf("session %q: %w", args[0], err)
			}
			return fmt.Errorf("revoke session: %w", err)
		}
		revoked = append(revoked, args[0])
	} else {
		active, err := store.List(ctx, storage.UISessionFilter{ActiveOnly: true, Now: now, PrincipalID: principal})
		if err != nil {
			return fmt.Errorf("list sessions: %w", err)
		}
		for _, s := range active {
			if err := store.Revoke(ctx, s.ID, now, authn.RevokeReasonRevoked); err != nil {
				return fmt.Errorf("revoke session %s: %w", s.ID, err)
			}
			revoked = append(revoked, s.ID)
		}
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{"revoked": revoked, "total": len(revoked)})
	}
	if len(revoked) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No active sessions for %s\n", principal)
		return nil
	}
	for _, id := range revoked {
		fmt.Fprintf(cmd.OutOrStdout(), "Revoked %s\n", id)
	}
	return nil
}
