package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"hop.top/kit/go/core/xdg"

	"github.com/ideacrafterslabs/ctxt/internal/cli"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	ghapi "github.com/ideacrafterslabs/ctxt/internal/github"
	githubimporter "github.com/ideacrafterslabs/ctxt/internal/importer/github"
)

// starsFetcher reads the viewer's stars and Lists; overridable in tests.
type starsFetcher interface {
	FetchStars(ctx context.Context, owner string) (*ghapi.StarSnapshot, error)
}

// starSubmitter hands one repository to the instance as a URL capture.
type starSubmitter func(ctx context.Context, req dpkmsclient.AnalyzeRequest) (string, error)

var (
	newStarsFetcher = func(_ *cobra.Command, token, baseURL string) starsFetcher {
		var opts []ghapi.Option
		if baseURL != "" {
			opts = append(opts, ghapi.WithBaseURL(baseURL))
		}
		if token != "" {
			return ghapi.NewClient(append(opts, ghapi.WithTokens(ghapi.StaticToken(token)))...)
		}
		if c := githubClient(os.Stderr); c != nil && baseURL == "" {
			return c
		}
		return ghapi.NewClientFromConfig(config.GitHubConfig{}, nil, opts...)
	}
	newStarSubmitter = func(cmd *cobra.Command) (starSubmitter, error) {
		dc, err := newDpkmsClient(cmd, 0)
		if err != nil {
			return nil, err
		}
		return dc.Analyze, nil
	}
	// starStatePath is where the incremental state lives; tests point it
	// at a temp dir.
	starStatePath = defaultStarStatePath
)

func addGitHubStarFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("star-lists", false, "import the account's starred repos and GitHub Lists (GraphQL)")
	cmd.Flags().String("account", "", "github.accounts[] name (or owner) whose token is used with --star-lists")
	cmd.Flags().Bool("full", false, "with --star-lists: resubmit every repo, ignoring the incremental state")
}

// resolveStarOwner maps --account to the owner the token source resolves:
// an account's name to its first owner, an owner login to itself. Empty
// means the default credential.
func resolveStarOwner(gh config.GitHubConfig, account string) (string, error) {
	account = strings.TrimSpace(account)
	if account == "" {
		return "", nil
	}
	for _, a := range gh.Accounts {
		if strings.EqualFold(a.Name, account) {
			if len(a.Owners) == 0 {
				return "", fmt.Errorf("github account %q lists no owners", a.Name)
			}
			return a.Owners[0], nil
		}
	}
	for _, a := range gh.Accounts {
		for _, o := range a.Owners {
			if strings.EqualFold(o, account) {
				return o, nil
			}
		}
	}
	return "", fmt.Errorf("unknown github account %q (github.accounts[] has no such name or owner)", account)
}

func defaultStarStatePath(instance, profile, viewer string) (string, error) {
	dir, err := xdg.RawStateDir("ctxt")
	if err != nil {
		return "", err
	}
	stem := strings.Join([]string{"github-stars", url.PathEscape(instance), url.PathEscape(profile), url.PathEscape(viewer)}, ".")
	return filepath.Join(dir, "import", stem+".json"), nil
}

const (
	starUnchanged = "unchanged"
	starFailed    = "failed"
)

// starRow is one repository of the report.
type starRow struct {
	SourceKey string   `json:"source_key"`
	Status    string   `json:"status"` // new | changed | unchanged | failed
	Hints     []string `json:"hints,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// starReport is what the command prints.
type starReport struct {
	Account   string    `json:"account"`
	DryRun    bool      `json:"dry_run"`
	Repos     int       `json:"repos"`
	Lists     int       `json:"lists"`
	Submitted int       `json:"submitted"`
	Unchanged int       `json:"unchanged"`
	Failed    int       `json:"failed"`
	Items     []starRow `json:"items"`
}

func runImportGitHubStars(cmd *cobra.Command) error {
	token, _ := cmd.Flags().GetString("token")
	if strings.TrimSpace(token) == "" {
		token = strings.TrimSpace(os.Getenv(githubTokenEnv))
	}
	account, _ := cmd.Flags().GetString("account")
	var ghCfg config.GitHubConfig
	if cfg != nil {
		ghCfg = cfg.GitHub
	}
	owner, err := resolveStarOwner(ghCfg, account)
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	full, _ := cmd.Flags().GetBool("full")
	outputFmt, _ := cmd.Flags().GetString("format")
	baseURL, _ := cmd.Flags().GetString("github-base-url")

	// An explicit account must use its configured credential, not a stray
	// GITHUB_TOKEN.
	if owner != "" {
		token = ""
	}
	snap, err := newStarsFetcher(cmd, token, baseURL).FetchStars(cmdContext(cmd), owner)
	if err != nil {
		return fmt.Errorf("github stars: %w", err)
	}
	items := githubimporter.BuildStarItems(snap)

	profile := activeProfileName(cmd)
	instance := ""
	if r, err := resolveEndpoint(cmd); err == nil {
		instance = r.Key
	}
	statePath, err := starStatePath(instance, profile, snap.Viewer)
	if err != nil {
		return err
	}
	state, err := githubimporter.LoadStarState(statePath)
	if err != nil {
		return err
	}
	plan := state.Plan(items, full)
	inPlan := map[string]bool{}
	for _, it := range plan {
		inPlan[it.SourceKey] = true
	}

	rep := starReport{Account: snap.Viewer, DryRun: dryRun, Repos: len(items), Lists: len(snap.Lists)}
	rows := map[string]*starRow{}
	for _, it := range items {
		status := starUnchanged
		if inPlan[it.SourceKey] {
			status = "changed"
			if _, seen := state.Keys[it.SourceKey]; !seen {
				status = "new"
			}
		}
		r := &starRow{SourceKey: it.SourceKey, Status: status, Hints: it.Hints}
		rows[it.SourceKey] = r
		rep.Items = append(rep.Items, *r)
	}

	var firstErr error
	if !dryRun && len(plan) > 0 {
		submit, err := newStarSubmitter(cmd)
		if err != nil {
			return err
		}
		failed := map[string]string{}
		for _, it := range plan {
			_, err := submit(cmdContext(cmd), dpkmsclient.AnalyzeRequest{
				Content:   it.URL,
				Type:      objectTypeURL,
				Source:    cli.SourceArgument,
				SourceKey: it.SourceKey,
				Hints:     it.Hints,
				Data:      it.Data,
				Profile:   profile,
			})
			if err != nil {
				failed[it.SourceKey] = err.Error()
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			state.Record(it)
		}
		if err := state.Save(); err != nil {
			return err
		}
		for i := range rep.Items {
			if msg, bad := failed[rep.Items[i].SourceKey]; bad {
				rep.Items[i].Status, rep.Items[i].Error = starFailed, msg
				rep.Failed++
			}
		}
	}
	for _, r := range rep.Items {
		switch r.Status {
		case starUnchanged:
			rep.Unchanged++
		case "new", "changed":
			if !dryRun {
				rep.Submitted++
			}
		}
	}
	if dryRun {
		rep.Submitted = 0
	}
	if err := renderStarReport(os.Stdout, outputFmt, rep); err != nil {
		return err
	}
	if firstErr != nil {
		return fmt.Errorf("import completed with errors (first: %w)", firstErr)
	}
	return nil
}

func renderStarReport(w io.Writer, format string, rep starReport) error {
	if format == formatJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	if rep.DryRun {
		rows := make([][]string, 0, len(rep.Items))
		for _, r := range rep.Items {
			rows = append(rows, []string{r.SourceKey, r.Status, strings.Join(r.Hints, ",")})
		}
		printTable(w, []string{"SourceKey", "Status", "Hints"}, rows)
	}
	fmt.Fprintf(w, "Account: %s\nRepos: %d\nLists: %d\n", rep.Account, rep.Repos, rep.Lists)
	if rep.DryRun {
		would := 0
		for _, r := range rep.Items {
			if r.Status != starUnchanged {
				would++
			}
		}
		fmt.Fprintf(w, "Would submit: %d\nUnchanged: %d\n", would, rep.Unchanged)
		return nil
	}
	fmt.Fprintf(w, "Submitted: %d\nUnchanged: %d\n", rep.Submitted, rep.Unchanged)
	if rep.Failed > 0 {
		fmt.Fprintf(w, "Failed: %d\n", rep.Failed)
	}
	return nil
}
