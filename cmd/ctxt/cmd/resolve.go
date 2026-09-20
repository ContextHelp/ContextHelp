package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
)

// resolveCmd is the one-shot resolver contract for external consumers
// (agents, scripts, editor integrations). It wraps the existing
// retrieval paths — Service.GetObject for knowledge objects,
// EntityStore.Resolve (alias-aware) for entities — and adds nothing
// beyond ref dispatch and output shaping.
var resolveCmd = &cobra.Command{
	Use:   "resolve <ref>",
	Short: "Resolve a ref to its body and provenance (one-shot)",
	Long: `Resolve a single ref and print its content to stdout.

Ref forms:
  obj_<id>       knowledge object by ID
  @<slug>        entity by slug or alias (leading @ optional)
  <slug>         entity by slug or alias

Default output is markdown (body only, pipe-friendly). --format json
emits an envelope with the body plus a provenance object: source,
pipeline, content hash, and registry influences for objects; registry
URL, version hash, namespace, and content status for entities. No
other format is supported.

An entity whose content status is "thin" or "pending_pull" is an
index-only stub: its body is empty until the content is pulled, and a
warning is written to stderr.

Exit codes follow the CLI convention: 0 on success, 1 on any error
(including ref not found and an unsupported --format).

Examples:
  # Resolve a knowledge object to markdown
  ctxt resolve obj_12345678

  # Resolve an entity by @-ref
  ctxt resolve @ui.best-practice

  # Body + provenance for scripting
  ctxt resolve obj_12345678 --format json`,
	Args: cobra.ExactArgs(1),
	RunE: runResolve,
}

func init() {
	rootCmd.AddCommand(resolveCmd)

	cliconv.WithSideEffect(resolveCmd, cliconv.SideEffectRead)
	// "resolve" is not in the kit default verb table; tag explicitly.
	cliconv.WithIdempotency(resolveCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(resolveCmd, []cliconv.Example{
		{Title: "Resolve an object to markdown", Command: "ctxt resolve obj_12345678"},
		{Title: "Resolve an entity by @-ref", Command: "ctxt resolve @ui.best-practice"},
		{Title: "Body + provenance as JSON", Command: "ctxt resolve obj_12345678 --format json"},
	})
}

// resolveResult is the --format json envelope: the resolved body plus
// a provenance object identifying where the content came from.
// Field order is layout-optimized (govet fieldalignment); the JSON tags
// fix the wire order, so it is independent of declaration order.
type resolveResult struct {
	Ref        string            `json:"ref"`
	Kind       string            `json:"kind"` // "object" | "entity"
	Title      string            `json:"title,omitempty"`
	Body       string            `json:"body"`
	Provenance resolveProvenance `json:"provenance"`
}

type resolveProvenance struct {
	// Shared.
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	// Object provenance.
	Source      string `json:"source,omitempty"`
	Pipeline    string `json:"pipeline,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
	// Entity provenance.
	RegistryURL string `json:"registry_url,omitempty"`
	VersionHash string `json:"version_hash,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	// ContentStatus is "full" | "thin" | "pending_pull". A thin or
	// pending_pull entity is an index-only stub whose body is legitimately
	// empty until pulled, so consumers can distinguish "no content yet"
	// from "no content at all" rather than retrying an empty body forever.
	ContentStatus string `json:"content_status,omitempty"`
	// Object provenance (slice last for layout).
	RegistryInfluences []string `json:"registry_influences,omitempty"`
}

func runResolve(cmd *cobra.Command, args []string) error {
	ref := args[0]

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()

	var res resolveResult
	if strings.HasPrefix(ref, "obj_") {
		obj, err := svc.GetObject(ctx, ref)
		if err != nil {
			return fmt.Errorf("resolve object %q: %w", ref, err)
		}
		doc := projection.ProjectDocument(obj)
		body := doc.Body
		if body == "" && len(doc.Sections) > 0 {
			var b strings.Builder
			for _, s := range doc.Sections {
				if s.Title != "" {
					fmt.Fprintf(&b, "## %s\n\n", s.Title)
				}
				if s.Content != "" {
					b.WriteString(s.Content)
					b.WriteString("\n\n")
				}
			}
			body = strings.TrimSpace(b.String())
		}
		// A graph carrying no Section nodes (e.g. the text.short pipeline
		// runs no markdown_parser/sectioner) projects to an empty Body and
		// no Sections, even though TextContent holds the full body — the
		// same shape ProjectIndex patches for FTS. Fall back to flat text
		// before RawContent, which is empty for non-web-captured objects.
		if body == "" {
			body = obj.TextContent
		}
		if body == "" {
			body = obj.RawContent
		}
		title := ""
		if len(obj.Summaries) > 0 {
			title = obj.Summaries[0]
		}
		res = resolveResult{
			Ref:   ref,
			Kind:  "object",
			Title: title,
			Body:  body,
			Provenance: resolveProvenance{
				Source:             obj.Source,
				Pipeline:           obj.Pipeline,
				ContentHash:        obj.ContentHash,
				RegistryInfluences: obj.RegistryInfluences,
				CreatedAt:          obj.CreatedAt,
				UpdatedAt:          obj.UpdatedAt,
			},
		}
	} else {
		slug := strings.TrimPrefix(ref, "@")
		entity, err := svc.Store.Entities().Resolve(ctx, slug)
		if err != nil {
			return fmt.Errorf("resolve entity %q: %w", ref, err)
		}
		// An index-only stub has no body to resolve. Warn on stderr so the
		// condition is visible to markdown consumers too (stdout stays
		// pipe-clean); the JSON envelope reports it as content_status.
		if entity.ContentStatus == storage.ContentStatusThin ||
			entity.ContentStatus == storage.ContentStatusPendingPull {
			fmt.Fprintf(os.Stderr,
				"warning: entity %q is %s (index-only stub); body is empty until pulled\n",
				slug, entity.ContentStatus)
		}
		res = resolveResult{
			Ref:   ref,
			Kind:  "entity",
			Title: entity.Title,
			Body:  entity.Description,
			Provenance: resolveProvenance{
				RegistryURL:   entity.RegistryURL,
				VersionHash:   entity.VersionHash,
				Namespace:     entity.Namespace,
				ContentStatus: string(entity.ContentStatus),
				CreatedAt:     entity.CreatedAt,
				UpdatedAt:     entity.UpdatedAt,
			},
		}
	}

	// Read --format from the inherited kit persistent flag (same pattern
	// as show.go) — cobra resolves inherited persistent flags through
	// Flags(). isJSONOutput() additionally honors the viper-bound value
	// set by the --output shim.
	format, _ := cmd.Flags().GetString("format") //nolint:errcheck // flag is registered by kit; absence yields "" and the markdown default
	if format == "json" || isJSONOutput() {
		return outputJSON(os.Stdout, res)
	}
	// Anything other than json/markdown is unsupported: reject it rather
	// than silently emitting markdown under a success exit code. kit v0.5
	// reports an unset --format as "table"; treat that (and "") as the
	// markdown default.
	if format != "" && format != "table" && format != "markdown" && format != "md" {
		return fmt.Errorf("unsupported format %q for resolve (want: markdown, json)", format)
	}

	// Markdown (default; kit v0.5 reports unset --format as "table").
	if res.Title != "" {
		fmt.Printf("# %s\n\n", res.Title)
	}
	if res.Body != "" {
		fmt.Println(res.Body)
	}
	return nil
}
