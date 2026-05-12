package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	dropboximporter "github.com/ideacrafterslabs/ctxt/internal/importer/dropbox"
	"github.com/spf13/cobra"
)

const dropboxAccessTokenEnv = "DROPBOX_ACCESS_TOKEN"

var importDropboxCmd = &cobra.Command{
	Use:   "dropbox",
	Short: "Import Dropbox files",
	Long: `Import selected Dropbox files into ctxt via dpkms enqueue.

What you must provide:
  1) A Dropbox OAuth access token with files.content.read scope
     - pass via --access-token or DROPBOX_ACCESS_TOKEN

How import scope is selective:
  - --path limits the folder (default: root)
  - --since RFC3339 or YYYY-MM-DD (client-side modified time filter)
  - --cursor resumes an incremental sync from a previous run
  - --recursive descends into sub-folders
  - --max-items caps total imported files

Examples:
  # Preview what would be imported from /documents in the last 30 days
  ctxt import dropbox --access-token $DROPBOX_ACCESS_TOKEN --path /documents --since 2026-01-19 --dry-run

  # Resume an incremental sync using a saved cursor
  ctxt import dropbox --access-token $DROPBOX_ACCESS_TOKEN --cursor <saved-cursor>

  # Import all Dropbox files, limit to 500
  ctxt import dropbox --access-token $DROPBOX_ACCESS_TOKEN --recursive --max-items 500 --server http://localhost:8080`,
	RunE: runImportDropbox,
}

func init() {
	importCmd.AddCommand(importDropboxCmd)

	importDropboxCmd.Flags().String("access-token", "", "Dropbox OAuth access token (or set DROPBOX_ACCESS_TOKEN)")
	importDropboxCmd.Flags().String("dropbox-api-url", "", "Dropbox API base URL override (tests/dev only)")
	importDropboxCmd.Flags().String("dropbox-content-url", "", "Dropbox content base URL override (tests/dev only)")
	importDropboxCmd.Flags().String("path", "", "folder path to import (default: root)")
	importDropboxCmd.Flags().String("since", "", "import files modified on/after this time (RFC3339 or YYYY-MM-DD)")
	importDropboxCmd.Flags().String("cursor", "", "resume sync from this cursor (incremental mode)")
	importDropboxCmd.Flags().Bool("recursive", false, "recurse into sub-folders")
	importDropboxCmd.Flags().Int("max-items", 0, "maximum number of files to import (0 = all)")
	importDropboxCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	importDropboxCmd.Flags().String("pipeline", "", "pipeline override for enqueued jobs")

	cliconv.WithSideEffect(importDropboxCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(importDropboxCmd, cliconv.IdempotencyConditional)

	cliconv.WithExamples(importDropboxCmd, []cliconv.Example{
		{
			Title:   "Import from default source",
			Command: "ctxt import dropbox --access-token $DROPBOX_ACCESS_TOKEN",
		},
		{
			Title:   "Dry-run preview",
			Command: "ctxt import dropbox --access-token $DROPBOX_ACCESS_TOKEN --confirm=no --dry-run",
		},
	})
	cliconv.WithNextSteps(importDropboxCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt list", Reason: "verify imported items"},
		{When: "on success", Suggest: "ctxt find <keyword>", Reason: "search the newly-imported data"},
	})
}

func runImportDropbox(cmd *cobra.Command, args []string) error {
	accessToken, _ := cmd.Flags().GetString("access-token")
	apiURL, _ := cmd.Flags().GetString("dropbox-api-url")
	contentURL, _ := cmd.Flags().GetString("dropbox-content-url")
	folderPath, _ := cmd.Flags().GetString("path")
	sinceRaw, _ := cmd.Flags().GetString("since")
	cursor, _ := cmd.Flags().GetString("cursor")
	recursive, _ := cmd.Flags().GetBool("recursive")
	maxItems, _ := cmd.Flags().GetInt("max-items")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	serverURL, _ := cmd.Flags().GetString("server")
	pipelineName, _ := cmd.Flags().GetString("pipeline")

	if strings.TrimSpace(accessToken) == "" {
		accessToken = strings.TrimSpace(os.Getenv(dropboxAccessTokenEnv))
	}
	if accessToken == "" {
		return fmt.Errorf("dropbox token is required: use --access-token or %s (scope: files.content.read)", dropboxAccessTokenEnv)
	}

	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	serverURL = strings.TrimRight(serverURL, "/")

	since, err := parseSinceValue(sinceRaw)
	if err != nil {
		return err
	}

	client := dropboximporter.NewClient(nil, apiURL, contentURL, accessToken)

	opts := dropboximporter.ListOptions{
		Path:      folderPath,
		Cursor:    cursor,
		Recursive: recursive,
		Since:     since,
		MaxItems:  maxItems,
	}

	result, err := client.ListFiles(context.Background(), opts)
	if err != nil {
		return err
	}

	if len(result.Files) == 0 {
		fmt.Println("No Dropbox files matched the selected scope.")
		return nil
	}

	fmt.Printf("Dropbox scope: %s\n", describeDropboxScope(opts))

	if dryRun {
		const previewLimit = 20
		fmt.Printf("Dropbox files matched: %d (dry-run)\n", len(result.Files))
		if result.Cursor != "" {
			fmt.Printf("Cursor for next incremental run: %s\n", result.Cursor)
		}
		preview := len(result.Files)
		if preview > previewLimit {
			preview = previewLimit
		}
		for i := 0; i < preview; i++ {
			f := result.Files[i]
			kind := "file"
			if f.IsFolder {
				kind = "folder"
			}
			mod := "-"
			if !f.ModifiedTime.IsZero() {
				mod = f.ModifiedTime.UTC().Format(time.RFC3339)
			}
			fmt.Printf("%d. [%s] %s (path=%s, modified=%s)\n", i+1, kind, f.Name, f.Path, mod)
		}
		if len(result.Files) > previewLimit {
			fmt.Printf("... and %d more files\n", len(result.Files)-previewLimit)
		}
		return nil
	}

	var (
		scanned  int
		imported int
		skipped  int
		failed   int
		firstErr error
	)

	for _, f := range result.Files {
		scanned++

		if f.IsFolder {
			skipped++
			continue
		}
		if !dropboximporter.IsSupportedExtension(f.Path) {
			skipped++
			continue
		}

		payload := renderDropboxText(f)
		_, err = enqueueImportItem(serverURL, payload, "text", pipelineName, "import:dropbox")
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		imported++
	}

	fmt.Printf("Dropbox files scanned: %d\n", scanned)
	fmt.Printf("Imported: %d\n", imported)
	fmt.Printf("Skipped: %d\n", skipped)
	if failed > 0 {
		fmt.Printf("Failed: %d\n", failed)
	}
	if result.Cursor != "" {
		fmt.Printf("Cursor for next incremental run: %s\n", result.Cursor)
	}

	if firstErr != nil {
		return fmt.Errorf("dropbox import completed with errors (first error: %w)", firstErr)
	}

	return nil
}

func describeDropboxScope(opts dropboximporter.ListOptions) string {
	parts := []string{}
	if opts.Path != "" {
		parts = append(parts, "path="+opts.Path)
	}
	if opts.Since != nil {
		parts = append(parts, "since="+opts.Since.UTC().Format(time.RFC3339))
	}
	if opts.Cursor != "" {
		parts = append(parts, "cursor=<incremental>")
	}
	if opts.Recursive {
		parts = append(parts, "recursive=true")
	}
	if opts.MaxItems > 0 {
		parts = append(parts, fmt.Sprintf("max-items=%d", opts.MaxItems))
	}
	if len(parts) == 0 {
		return "all files in root"
	}
	return strings.Join(parts, "; ")
}

func renderDropboxText(f dropboximporter.File) string {
	var b strings.Builder
	title := strings.TrimSpace(f.Name)
	if title == "" {
		title = f.Path
	}
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString("Source: dropbox\n")
	b.WriteString("External ID: ")
	b.WriteString(f.ID)
	b.WriteString("\n")
	b.WriteString("Path: ")
	b.WriteString(f.Path)
	b.WriteString("\n")
	if !f.ModifiedTime.IsZero() {
		b.WriteString("Modified: ")
		b.WriteString(f.ModifiedTime.UTC().Format(time.RFC3339))
		b.WriteString("\n")
	}
	if f.ContentHash != "" {
		b.WriteString("Content-Hash: ")
		b.WriteString(f.ContentHash)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return b.String()
}
