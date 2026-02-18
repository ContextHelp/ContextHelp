package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	gdriveimporter "github.com/ideacrafterslabs/ctxt/internal/importer/gdrive"
	"github.com/spf13/cobra"
)

const gdriveAccessTokenEnv = "GDRIVE_ACCESS_TOKEN"

var importGDriveCmd = &cobra.Command{
	Use:   "gdrive",
	Short: "Import Google Drive files",
	Long: `Import selected Google Drive files into ctxt via dpkms enqueue.

What you must provide:
  1) A Google OAuth access token with at least drive.readonly scope
     - pass via --access-token or GDRIVE_ACCESS_TOKEN
  2) A reachable dpkms server (--server) unless using dry-run

How import scope is selective:
  - --since RFC3339 or YYYY-MM-DD (modified time floor)
  - --folder-id can be repeated to limit parent folders
  - --mime-type can be repeated to limit file types
  - --query passes raw Drive API q filters for advanced targeting
  - --max-items caps total imported files

Examples:
  # Preview what would be imported from a folder in the last 30 days
  ctxt import gdrive --access-token $GDRIVE_ACCESS_TOKEN --folder-id abc123 --since 2026-01-19 --dry-run

  # Import only Google Docs and PDFs, limit to 200 files
  ctxt import gdrive --access-token $GDRIVE_ACCESS_TOKEN --mime-type application/vnd.google-apps.document --mime-type application/pdf --max-items 200 --server http://localhost:8080`,
	RunE: runImportGDrive,
}

func init() {
	importCmd.AddCommand(importGDriveCmd)

	importGDriveCmd.Flags().String("access-token", "", "Google OAuth access token (or set GDRIVE_ACCESS_TOKEN)")
	importGDriveCmd.Flags().String("drive-base-url", "", "Google Drive API base URL override (tests/dev only)")
	importGDriveCmd.Flags().String("query", "", "raw Drive API query expression (q)")
	importGDriveCmd.Flags().String("since", "", "import files modified on/after this time (RFC3339 or YYYY-MM-DD)")
	importGDriveCmd.Flags().StringSlice("folder-id", nil, "only import files in these parent folder IDs")
	importGDriveCmd.Flags().StringSlice("mime-type", nil, "only import these MIME types")
	importGDriveCmd.Flags().Bool("include-trashed", false, "include trashed files (default false)")
	importGDriveCmd.Flags().Int("max-items", 0, "maximum number of files to import (0 = all)")
	importGDriveCmd.Flags().Bool("dry-run", false, "preview matched files without enqueueing jobs")
	importGDriveCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	importGDriveCmd.Flags().String("pipeline", "", "pipeline override for enqueued jobs")
}

func runImportGDrive(cmd *cobra.Command, args []string) error {
	accessToken, _ := cmd.Flags().GetString("access-token")
	driveBaseURL, _ := cmd.Flags().GetString("drive-base-url")
	query, _ := cmd.Flags().GetString("query")
	sinceRaw, _ := cmd.Flags().GetString("since")
	folderIDs, _ := cmd.Flags().GetStringSlice("folder-id")
	mimeTypes, _ := cmd.Flags().GetStringSlice("mime-type")
	includeTrashed, _ := cmd.Flags().GetBool("include-trashed")
	maxItems, _ := cmd.Flags().GetInt("max-items")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	serverURL, _ := cmd.Flags().GetString("server")
	pipelineName, _ := cmd.Flags().GetString("pipeline")

	if strings.TrimSpace(accessToken) == "" {
		accessToken = strings.TrimSpace(os.Getenv(gdriveAccessTokenEnv))
	}
	if accessToken == "" {
		return fmt.Errorf("google drive token is required: use --access-token or %s (scope: drive.readonly)", gdriveAccessTokenEnv)
	}

	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	serverURL = strings.TrimRight(serverURL, "/")

	since, err := parseSinceValue(sinceRaw)
	if err != nil {
		return err
	}

	client := gdriveimporter.NewClient(nil, driveBaseURL, accessToken)
	listOpts := gdriveimporter.ListOptions{
		Query:          query,
		FolderIDs:      folderIDs,
		MimeTypes:      mimeTypes,
		Since:          since,
		IncludeTrashed: includeTrashed,
		MaxItems:       maxItems,
	}

	files, err := client.ListFiles(context.Background(), listOpts)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no Google Drive files matched the selected scope")
	}

	fmt.Printf("GDrive scope: %s\n", describeGDriveScope(listOpts))

	if dryRun {
		const previewLimit = 20
		fmt.Printf("GDrive files matched: %d (dry-run)\n", len(files))
		preview := len(files)
		if preview > previewLimit {
			preview = previewLimit
		}
		for i := 0; i < preview; i++ {
			f := files[i]
			mod := "-"
			if !f.ModifiedTime.IsZero() {
				mod = f.ModifiedTime.UTC().Format(time.RFC3339)
			}
			fmt.Printf("%d. [%s] %s (id=%s, modified=%s)\n", i+1, f.MimeType, f.Name, f.ID, mod)
		}
		if len(files) > previewLimit {
			fmt.Printf("... and %d more files\n", len(files)-previewLimit)
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

	for _, f := range files {
		scanned++

		payload, contentType, err := gdrivePayloadForFile(context.Background(), client, f)
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if payload == "" {
			skipped++
			continue
		}

		_, err = enqueueImportItem(serverURL, payload, contentType, pipelineName, "import:gdrive")
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		imported++
	}

	fmt.Printf("GDrive files scanned: %d\n", scanned)
	fmt.Printf("Imported: %d\n", imported)
	fmt.Printf("Skipped: %d\n", skipped)
	if failed > 0 {
		fmt.Printf("Failed: %d\n", failed)
	}

	if firstErr != nil {
		return fmt.Errorf("gdrive import completed with errors (first error: %w)", firstErr)
	}

	return nil
}

func parseSinceValue(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	parsed, err := time.Parse(time.RFC3339, raw)
	if err == nil {
		return &parsed, nil
	}

	parsed, err = time.Parse("2006-01-02", raw)
	if err == nil {
		return &parsed, nil
	}

	return nil, fmt.Errorf("invalid --since value %q: expected RFC3339 or YYYY-MM-DD", raw)
}

func describeGDriveScope(opts gdriveimporter.ListOptions) string {
	parts := []string{}
	if opts.Since != nil {
		parts = append(parts, "since="+opts.Since.UTC().Format(time.RFC3339))
	}
	if len(opts.FolderIDs) > 0 {
		parts = append(parts, "folders="+strings.Join(opts.FolderIDs, ","))
	}
	if len(opts.MimeTypes) > 0 {
		parts = append(parts, "mime="+strings.Join(opts.MimeTypes, ","))
	}
	if strings.TrimSpace(opts.Query) != "" {
		parts = append(parts, "query="+opts.Query)
	}
	if opts.MaxItems > 0 {
		parts = append(parts, fmt.Sprintf("max-items=%d", opts.MaxItems))
	}
	if opts.IncludeTrashed {
		parts = append(parts, "include-trashed=true")
	}
	if len(parts) == 0 {
		return "all visible files"
	}
	return strings.Join(parts, "; ")
}

func gdrivePayloadForFile(ctx context.Context, client *gdriveimporter.Client, f gdriveimporter.File) (string, string, error) {
	exportMime, canExport := gdriveExportMimeForFile(f.MimeType)
	if canExport {
		body, err := client.ExportText(ctx, f.ID, exportMime)
		if err != nil {
			return "", "", fmt.Errorf("export %s (%s): %w", f.Name, f.ID, err)
		}
		payload := renderGDriveText(f, body)
		return payload, "text", nil
	}

	if strings.TrimSpace(f.WebViewLink) != "" {
		return f.WebViewLink, "url", nil
	}
	if strings.TrimSpace(f.WebContentLink) != "" {
		return f.WebContentLink, "url", nil
	}

	return "", "", nil
}

func gdriveExportMimeForFile(fileMime string) (string, bool) {
	switch fileMime {
	case "application/vnd.google-apps.document":
		return "text/plain", true
	case "application/vnd.google-apps.spreadsheet":
		return "text/csv", true
	case "application/vnd.google-apps.presentation":
		return "text/plain", true
	default:
		return "", false
	}
}

func renderGDriveText(f gdriveimporter.File, body string) string {
	var b strings.Builder
	title := strings.TrimSpace(f.Name)
	if title == "" {
		title = f.ID
	}
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString("Source: gdrive\n")
	b.WriteString("External ID: ")
	b.WriteString(f.ID)
	b.WriteString("\n")
	if f.WebViewLink != "" {
		b.WriteString("Drive URL: ")
		b.WriteString(f.WebViewLink)
		b.WriteString("\n")
	}
	if !f.ModifiedTime.IsZero() {
		b.WriteString("Modified: ")
		b.WriteString(f.ModifiedTime.UTC().Format(time.RFC3339))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n")
	return b.String()
}

func enqueueImportItem(serverURL, content, contentType, pipelineName, source string) (string, error) {
	payload := enqueueRequest{
		Content:  content,
		Type:     contentType,
		Pipeline: pipelineName,
		Source:   source,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	endpoints := []string{"/api/v1/pipelines/enqueue", "/api/v1/analyze"}
	for i, endpoint := range endpoints {
		jobID, statusCode, respBody, err := postEnqueueRequest(serverURL+endpoint, body)
		if err != nil {
			return "", err
		}
		if statusCode == 404 && i == 0 {
			continue
		}
		if statusCode != 202 && statusCode != 200 {
			return "", fmt.Errorf("dpkms returned %d: %s", statusCode, strings.TrimSpace(respBody))
		}
		return jobID, nil
	}

	return "", fmt.Errorf("enqueue failed: endpoint unavailable")
}
