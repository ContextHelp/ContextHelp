package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/cli"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage principals and their credentials on this host",
	Long: `Manage the principal registry of this instance directly in its database.

These commands run on the dpkms host and open the configured store, like
'dpkms session'. They are the bootstrap and the recovery path: an owner
with no working credential creates one here, with filesystem access to
the host as the root of trust. A running dpkms sees a new key on its
next request.

Examples:
  # First owner API key on a fresh protected instance (dpkms stopped)
  dpkms auth key create --principal owner --name "laptop CLI" --maintenance-override`,
}

var authKeyCmd = &cobra.Command{
	Use:   "key",
	Short: "Manage API keys",
}

var authKeyCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an API key for a principal",
	Long: `Create an API key for a principal and print it once.

The key goes to stdout alone, so it can be captured; a one-line
confirmation goes to stderr. Only the key's hash is stored: a lost key is
revoked and replaced, never recovered.

The key holds the principal's role scopes. It never expires unless
--expires gives a duration (720h) or a date (2026-12-31, or RFC 3339;
a date expires at the end of that day, UTC). The principal must exist
and be active; 'owner' always exists.

Examples:
  # Owner key, no expiry
  dpkms auth key create --principal owner --name "laptop CLI"

  # Capture a key that expires in 30 days
  KEY=$(dpkms auth key create --principal owner --expires 720h)`,
	Args: cobra.NoArgs,
	RunE: runAuthKeyCreate,
}

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.AddCommand(authKeyCmd)
	authKeyCmd.AddCommand(authKeyCreateCmd)

	authKeyCreateCmd.Flags().String("principal", "", "principal the key acts for (owner, or a prn_ id)")
	authKeyCreateCmd.Flags().String("name", "", "label for the key (\"laptop CLI\")")
	authKeyCreateCmd.Flags().String("expires", "", "expiry as a duration (720h) or a date (2026-12-31); never by default")
	markFlagsRequired(authKeyCreateCmd, "principal")
	// The bootstrap path must reach a protected instance too: audited.
	addMaintenanceOverrideFlag(authKeyCreateCmd, false)

	// Creating a key adds a credential; every run mints a different
	// one. Write, not idempotent.
	cliconv.WithSideEffect(authKeyCreateCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(authKeyCreateCmd, cliconv.IdempotencyNo)
}

// parseKeyExpiry reads --expires: empty never expires, a duration (720h,
// 30d) counts from now, a date (2006-01-02) expires at the end of that
// UTC day, an RFC 3339 time is taken as given.
func parseKeyExpiry(s string, now time.Time) (time.Time, error) {
	return cli.ParseExpiry(s, now)
}

func runAuthKeyCreate(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	principal, _ := cmd.Flags().GetString("principal")
	name, _ := cmd.Flags().GetString("name")
	expires, _ := cmd.Flags().GetString("expires")
	if strings.TrimSpace(principal) == "" {
		return errors.New("--principal is required")
	}
	expiresAt, err := parseKeyExpiry(expires, time.Now().UTC())
	if err != nil {
		return err
	}

	driver, cleanup, err := openStore(cmd)
	if err != nil {
		return err
	}
	defer cleanup()

	secret, issued, err := authn.NewKeysOver(driver.Auth(), nil).Mint(ctx, authn.MintKey{
		PrincipalID: strings.TrimSpace(principal), Name: name, ExpiresAt: expiresAt,
	})
	if err != nil {
		return fmt.Errorf("create api key: %w", err)
	}

	if isJSONOutput() {
		doc := map[string]any{
			"key":          secret,
			"id":           issued.ID,
			"principal_id": issued.PrincipalID,
			"identity_id":  issued.IdentityID,
			"name":         issued.Name,
			"scopes":       issued.Scopes,
			"created_at":   issued.CreatedAt,
		}
		if !issued.ExpiresAt.IsZero() {
			doc["expires_at"] = issued.ExpiresAt
		}
		return outputJSON(cmd.OutOrStdout(), doc)
	}
	fmt.Fprintln(cmd.OutOrStdout(), secret)
	var detail strings.Builder
	if issued.Name != "" {
		fmt.Fprintf(&detail, " (%s)", issued.Name)
	}
	if issued.ExpiresAt.IsZero() {
		detail.WriteString(", no expiry")
	} else {
		fmt.Fprintf(&detail, ", expires %s", issued.ExpiresAt.UTC().Format(time.RFC3339))
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Created API key %s for principal %s%s. Shown once; only its hash is stored.\n",
		issued.ID, issued.PrincipalID, detail.String())
	return nil
}
