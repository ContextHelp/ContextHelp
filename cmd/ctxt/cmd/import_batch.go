package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	gohttp "net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var importBatchCmd = &cobra.Command{
	Use:   "batch",
	Short: "Import records from a file or directory",
	Long: `Import knowledge records from structured files or a directory of markdown files.

Supported file formats:
  - JSONL (.jsonl) — one JSON object per line
  - CSV  (.csv)   — comma-separated values
  - TSV  (.tsv)   — tab-separated values
  - Markdown (.md) — individual markdown documents (use --dir for batch)

Examples:
  # Import a JSONL file
  ctxt import batch --file records.jsonl

  # Dry-run import of a CSV file with column mappings
  ctxt import batch --file data.csv --map-content body --map-type kind --dry-run

  # Import all markdown files in a directory
  ctxt import batch --dir ./notes --format markdown

  # Check status of a batch import
  ctxt import batch status batch_12345678`,
	RunE: runImportBatch,
}

var importBatchStatusCmd = &cobra.Command{
	Use:   "status <batch-id>",
	Short: "Check the status of a batch import",
	Long:  `Retrieve the current status of an ongoing or completed batch import.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runImportBatchStatus,
}

func init() {
	importCmd.AddCommand(importBatchCmd)
	importBatchCmd.AddCommand(importBatchStatusCmd)

	// batch flags
	importBatchCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	importBatchCmd.Flags().String("file", "", "path to import file (JSONL, CSV, TSV)")
	importBatchCmd.Flags().String("dir", "", "directory to scan for markdown files")
	importBatchCmd.Flags().String("format", "", "file format: jsonl|csv|tsv|markdown (auto-detected from extension)")
	importBatchCmd.Flags().Bool("dry-run", false, "validate without importing (sends dry_run=true)")
	importBatchCmd.Flags().String("map-content", "", "column name to use as content (CSV/TSV imports)")
	importBatchCmd.Flags().String("map-type", "", "column name to use as type (CSV/TSV imports)")
	importBatchCmd.Flags().String("map-tags", "", "column name to use as tags (CSV/TSV imports)")

	// batch status flags
	importBatchStatusCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
}

// batchServerURL returns the server URL from the command flag or the default.
func batchServerURL(cmd *cobra.Command) string {
	url, _ := cmd.Flags().GetString("server")
	if url == "" {
		url = "http://localhost:8080"
	}
	return strings.TrimRight(url, "/")
}

// detectFormat returns the format string inferred from a filename extension.
func detectFormat(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jsonl":
		return "jsonl"
	case ".csv":
		return "csv"
	case ".tsv":
		return "tsv"
	case ".md", ".markdown":
		return "markdown"
	default:
		return ""
	}
}

func runImportBatch(cmd *cobra.Command, args []string) error {
	serverURL := batchServerURL(cmd)
	filePath, _ := cmd.Flags().GetString("file")
	dirPath, _ := cmd.Flags().GetString("dir")
	format, _ := cmd.Flags().GetString("format")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	mapContent, _ := cmd.Flags().GetString("map-content")
	mapType, _ := cmd.Flags().GetString("map-type")
	mapTags, _ := cmd.Flags().GetString("map-tags")

	if filePath == "" && dirPath == "" {
		return fmt.Errorf("either --file or --dir is required")
	}
	if filePath != "" && dirPath != "" {
		return fmt.Errorf("--file and --dir are mutually exclusive")
	}

	if dirPath != "" {
		return runImportDir(serverURL, dirPath, dryRun)
	}

	return runImportFile(serverURL, filePath, format, dryRun, mapContent, mapType, mapTags)
}

func runImportFile(serverURL, filePath, format string, dryRun bool, mapContent, mapType, mapTags string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	if format == "" {
		format = detectFormat(filePath)
	}
	if format == "" {
		return fmt.Errorf("cannot detect format from %q — use --format jsonl|csv|tsv|markdown", filePath)
	}

	payload := map[string]any{
		"content":  string(data),
		"format":   format,
		"dry_run":  dryRun,
		"filename": filepath.Base(filePath),
	}
	if mapContent != "" {
		payload["map_content"] = mapContent
	}
	if mapType != "" {
		payload["map_type"] = mapType
	}
	if mapTags != "" {
		payload["map_tags"] = mapTags
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	resp, err := gohttp.Post(serverURL+"/api/v1/import", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != gohttp.StatusAccepted && resp.StatusCode != gohttp.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if dryRun {
		fmt.Printf("Dry-run complete (format: %s)\n", format)
		if count, ok := result["count"]; ok {
			fmt.Printf("Records found: %v\n", count)
		}
		return nil
	}

	if batchID, ok := result["batch_id"].(string); ok && batchID != "" {
		fmt.Printf("Batch ID: %s\n", batchID)
	}
	if count, ok := result["count"]; ok {
		fmt.Printf("Records queued: %v\n", count)
	}
	return nil
}

func runImportDir(serverURL, dirPath string, dryRun bool) error {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("read directory: %w", err)
	}

	var mdFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext == ".md" || ext == ".markdown" {
			mdFiles = append(mdFiles, filepath.Join(dirPath, e.Name()))
		}
	}

	if len(mdFiles) == 0 {
		return fmt.Errorf("no markdown files found in %s", dirPath)
	}

	if dryRun {
		fmt.Printf("Dry-run: found %d markdown files in %s\n", len(mdFiles), dirPath)
		for _, f := range mdFiles {
			fmt.Printf("  %s\n", filepath.Base(f))
		}
		return nil
	}

	var (
		success  int
		failed   int
		firstErr error
	)

	for _, f := range mdFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		payload := map[string]any{
			"content":  string(data),
			"format":   "markdown",
			"filename": filepath.Base(f),
			"dry_run":  false,
		}
		body, err := json.Marshal(payload)
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		resp, err := gohttp.Post(serverURL+"/api/v1/import", "application/json", bytes.NewReader(body))
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		resp.Body.Close()

		if resp.StatusCode != gohttp.StatusAccepted && resp.StatusCode != gohttp.StatusOK {
			failed++
			if firstErr == nil {
				firstErr = fmt.Errorf("server returned %d for %s", resp.StatusCode, filepath.Base(f))
			}
			continue
		}
		success++
	}

	fmt.Printf("Directory: %s\n", dirPath)
	fmt.Printf("Files processed: %d\n", len(mdFiles))
	fmt.Printf("Files imported: %d\n", success)
	if failed > 0 {
		fmt.Printf("Files failed: %d\n", failed)
	}

	if firstErr != nil {
		return fmt.Errorf("import failed for %d files (first error: %w)", failed, firstErr)
	}
	return nil
}

// batchStatusResponse represents the API response for a batch import status query.
type batchStatusResponse struct {
	ID        string `json:"id"`
	Status    string `json:"status"`
	Total     int    `json:"total"`
	Processed int    `json:"processed"`
	Failed    int    `json:"failed"`
	CreatedAt string `json:"created_at"`
}

func runImportBatchStatus(cmd *cobra.Command, args []string) error {
	batchID := args[0]
	serverURL := batchServerURL(cmd)

	resp, err := gohttp.Get(serverURL + "/api/v1/import/" + batchID)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != gohttp.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var status batchStatusResponse
	if err := json.Unmarshal(respBody, &status); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), status)
	}

	fmt.Printf("Batch: %s\n\n", status.ID)
	fmt.Printf("Status:     %s\n", statusStyle(status.Status))
	fmt.Printf("Total:      %d\n", status.Total)
	fmt.Printf("Processed:  %d\n", status.Processed)
	fmt.Printf("Failed:     %d\n", status.Failed)
	if status.CreatedAt != "" {
		fmt.Printf("Created:    %s\n", status.CreatedAt)
	}
	return nil
}
