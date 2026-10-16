package cmd

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// activeProfileName: --profile when passed (empty selects none), else
// profile.default (config file or CTXT_PROFILE, merged into cfg), else none.
func TestActiveProfileName(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  *config.Config
		args []string
		want string
	}{
		{name: "no config, no flag", want: ""},
		{name: "no config, flag", args: []string{"--profile", "growth"}, want: "growth"},
		{name: "default, no flag", cfg: &config.Config{Profile: config.ProfileConfig{Default: "ops"}}, want: "ops"},
		{name: "flag over default", cfg: &config.Config{Profile: config.ProfileConfig{Default: "ops"}}, args: []string{"--profile", "growth"}, want: "growth"},
		{name: "empty flag selects none", cfg: &config.Config{Profile: config.ProfileConfig{Default: "ops"}}, args: []string{"--profile", ""}, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prev := cfg
			t.Cleanup(func() { cfg = prev })
			cfg = tc.cfg
			c := &cobra.Command{Use: "x"}
			c.Flags().String("profile", "", "")
			if err := c.ParseFlags(tc.args); err != nil {
				t.Fatal(err)
			}
			if got := activeProfileName(c); got != tc.want {
				t.Errorf("activeProfileName = %q, want %q", got, tc.want)
			}
		})
	}
}

// profileCase is one row of the per-command precedence tables: the
// config's profile.default, CTXT_PROFILE, the extra args, and the profile
// the command must run under ("" = none).
type profileCase struct {
	name       string
	configured string
	env        string
	args       []string
	want       string
}

var profileCases = []profileCase{
	{name: "none", want: ""},
	{name: "config default", configured: "ops", want: "ops"},
	{name: "env default", env: "ops", want: "ops"},
	{name: "env over config", configured: "growth", env: "ops", want: "ops"},
	{name: "flag", args: []string{"--profile", "growth"}, want: "growth"},
	{name: "flag over config", configured: "ops", args: []string{"--profile", "growth"}, want: "growth"},
	{name: "flag over env", env: "ops", args: []string{"--profile", "growth"}, want: "growth"},
	{name: "empty flag selects none", configured: "ops", args: []string{"--profile", ""}, want: ""},
}

// writeProfilesConfig appends a growth + ops profile block, with
// configured as profile.default, to db's ctxt config.
func writeProfilesConfig(t *testing.T, db *testDB, configured string) {
	t.Helper()
	raw, err := os.ReadFile(db.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw) + "profile:\n"
	if configured != "" {
		body += "  default: " + configured + "\n"
	}
	body += "  profiles:\n    growth: {}\n    ops: {}\n"
	if err := os.WriteFile(db.ConfigPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// find sends the resolved profile in POST /api/v1/find.
func TestFindProfilePrecedence(t *testing.T) {
	for _, tc := range profileCases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			t.Setenv(config.EnvProfile, tc.env)
			writeProfilesConfig(t, db, tc.configured)
			rec := recordFind(t, db, dpkmstest.RoleAdmin)
			args := append([]string{"find", "deploy", "--format", "json"}, tc.args...)
			if _, stderr, err := execFind(t, db, args...); err != nil {
				t.Fatalf("find: %v\n%s", err, stderr)
			}
			body := rec.last(t)
			got, sent := body["profile"]
			if tc.want == "" {
				if sent {
					t.Errorf("profile = %v, want none sent", got)
				}
				return
			}
			if got != tc.want {
				t.Errorf("profile = %v, want %q", got, tc.want)
			}
		})
	}
}

// resurface lists the resolved profile's queue: its JSON names that
// profile and every item belongs to it.
func TestResurfaceProfilePrecedence(t *testing.T) {
	for _, tc := range profileCases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t)
			t.Setenv(config.EnvProfile, tc.env)
			writeProfilesConfig(t, db, tc.configured)
			seedResurfacing(t, db, "growth", "ops")

			args := append([]string{"resurface", "--format", "json"}, tc.args...)
			stdout, stderr, err := execFind(t, db, args...)
			if err != nil {
				t.Fatalf("resurface: %v\n%s", err, stderr)
			}
			var out struct {
				Profile string                      `json:"profile"`
				Items   []*storage.ResurfacingEntry `json:"items"`
			}
			if err := json.Unmarshal([]byte(stdout), &out); err != nil {
				t.Fatalf("decode: %v\n%s", err, stdout)
			}
			if out.Profile != tc.want {
				t.Errorf("profile = %q, want %q", out.Profile, tc.want)
			}
			if tc.want == "" {
				return
			}
			if len(out.Items) != 1 || out.Items[0].ProfileID != tc.want {
				t.Errorf("items = %+v, want only %s's entry", out.Items, tc.want)
			}
		})
	}
}

// seedResurfacing queues one unseen entry per profile, each on its own
// object.
func seedResurfacing(t *testing.T, db *testDB, profiles ...string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	for _, p := range profiles {
		id := "obj-" + p
		if err := db.Driver.Objects().Create(ctx, &storage.KnowledgeObject{
			ID: id, Type: "text", Summaries: []string{p + " note"}, CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
		if err := db.Driver.Resurfacing().Upsert(ctx, &storage.ResurfacingEntry{
			ID: "rs-" + p, ObjectID: id, ProfileID: p, Score: 0.9, Reason: "test", CreatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
}
