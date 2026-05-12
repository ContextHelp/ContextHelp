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
	"gopkg.in/yaml.v3"
)

var importEmailCmd = &cobra.Command{
	Use:   "email",
	Short: "Import email messages from IMAP, .eml files, or mbox archives",
	Long: `Import email messages into dpkms from various sources.

Supported providers:
  imap    — IMAP4rev1 mailbox (with STARTTLS or TLS)
  file    — Local .eml file or mbox archive

Examples:
  # Import from IMAP with STARTTLS
  ctxt import email --provider imap --host imap.example.com --user me@example.com --password SECRET

  # Import from IMAP with TLS (port 993)
  ctxt import email --provider imap --host imap.gmail.com --user me@gmail.com --password SECRET --tls

  # Import a single .eml file
  ctxt import email --provider file --file message.eml

  # Import an mbox archive with custom rules
  ctxt import email --provider file --file archive.mbox --rules rules.yaml

  # Dry-run to see routing decisions without importing
  ctxt import email --provider file --file archive.mbox --dry-run

  # Limit to a specific folder and maximum number of messages
  ctxt import email --provider imap --host mail.example.com --user me@example.com --folder "INBOX/Newsletters" --max-items 100`,
	RunE: runImportEmail,
}

func init() {
	importCmd.AddCommand(importEmailCmd)

	f := importEmailCmd.Flags()
	f.String("server", "", "dpkms server URL (default http://localhost:8080)")
	f.String("provider", "file", "provider: imap|file")

	// IMAP flags.
	f.String("host", "", "IMAP server hostname or host:port")
	f.String("user", "", "IMAP username / email address")
	f.String("password", "", "IMAP password or app password")
	f.Bool("tls", false, "use implicit TLS (port 993); default is STARTTLS (port 143)")
	f.Int("port", 0, "IMAP port override (defaults to 993 for --tls, 143 otherwise)")
	f.String("folder", "INBOX", "IMAP mailbox/folder to sync")

	// File flags.
	f.String("file", "", "path to .eml file or mbox archive")

	// Common flags.
	f.String("rules", "", "path to YAML rules file for custom routing")
	f.String("pipeline", "", "pipeline override (applied to all messages, bypasses routing)")
	f.String("since", "", "only fetch messages after this date (RFC 3339 or YYYY-MM-DD)")
	f.Int("max-items", 0, "maximum number of messages to import (0 = unlimited)")
}

// emailServerURL returns the server URL from the command flag or the default.
func emailServerURL(cmd *cobra.Command) string {
	url, _ := cmd.Flags().GetString("server")
	if url == "" {
		url = "http://localhost:8080"
	}
	return strings.TrimRight(url, "/")
}

func runImportEmail(cmd *cobra.Command, args []string) error {
	provider, _ := cmd.Flags().GetString("provider")
	serverURL := emailServerURL(cmd)
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	rulesFile, _ := cmd.Flags().GetString("rules")
	pipelineOverride, _ := cmd.Flags().GetString("pipeline")
	maxItems, _ := cmd.Flags().GetInt("max-items")
	since, _ := cmd.Flags().GetString("since")

	// Load optional rules.
	var rulesPayload any
	if rulesFile != "" {
		data, err := os.ReadFile(rulesFile)
		if err != nil {
			return fmt.Errorf("read rules file: %w", err)
		}
		var rules any
		if err := yaml.Unmarshal(data, &rules); err != nil {
			return fmt.Errorf("parse rules file: %w", err)
		}
		rulesPayload = rules
	}

	switch strings.ToLower(provider) {
	case "imap":
		return runImportEmailIMAP(cmd, serverURL, dryRun, rulesPayload, pipelineOverride, maxItems, since)
	case "file":
		return runImportEmailFile(cmd, serverURL, dryRun, rulesPayload, pipelineOverride, maxItems)
	default:
		return fmt.Errorf("unknown provider %q; supported: imap, file", provider)
	}
}

func runImportEmailIMAP(cmd *cobra.Command, serverURL string, dryRun bool, rules any, pipelineOverride string, maxItems int, since string) error {
	host, _ := cmd.Flags().GetString("host")
	user, _ := cmd.Flags().GetString("user")
	password, _ := cmd.Flags().GetString("password")
	useTLS, _ := cmd.Flags().GetBool("tls")
	port, _ := cmd.Flags().GetInt("port")
	folder, _ := cmd.Flags().GetString("folder")

	if host == "" {
		return fmt.Errorf("--host is required for IMAP provider")
	}
	if user == "" {
		return fmt.Errorf("--user is required for IMAP provider")
	}
	if password == "" {
		return fmt.Errorf("--password is required for IMAP provider")
	}

	payload := map[string]any{
		"provider": "imap",
		"imap": map[string]any{
			"host":     host,
			"username": user,
			"password": password,
			"use_tls":  useTLS,
			"port":     port,
			"folder":   folder,
		},
		"dry_run":           dryRun,
		"pipeline_override": pipelineOverride,
		"max_items":         maxItems,
		"since":             since,
	}
	if rules != nil {
		payload["rules"] = rules
	}

	return sendEmailImportRequest(serverURL, payload, dryRun, cmd.OutOrStdout())
}

func runImportEmailFile(cmd *cobra.Command, serverURL string, dryRun bool, rules any, pipelineOverride string, maxItems int) error {
	filePath, _ := cmd.Flags().GetString("file")
	if filePath == "" {
		return fmt.Errorf("--file is required for file provider")
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	format := "eml"
	if ext == ".mbox" {
		format = "mbox"
	}

	payload := map[string]any{
		"provider": "file",
		"file": map[string]any{
			"content":  string(data),
			"filename": filepath.Base(filePath),
			"format":   format,
		},
		"dry_run":           dryRun,
		"pipeline_override": pipelineOverride,
		"max_items":         maxItems,
	}
	if rules != nil {
		payload["rules"] = rules
	}

	return sendEmailImportRequest(serverURL, payload, dryRun, cmd.OutOrStdout())
}

func sendEmailImportRequest(serverURL string, payload map[string]any, dryRun bool, out io.Writer) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	resp, err := gohttp.Post(serverURL+"/api/v1/importers/email/run", "application/json", bytes.NewReader(body))
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
		fmt.Fprintln(out, "Dry-run complete")
		printEmailImportResult(out, result)
		return nil
	}

	if runID, ok := result["run_id"].(string); ok && runID != "" {
		fmt.Fprintf(out, "Import run: %s\n", runID)
	}
	printEmailImportResult(out, result)
	return nil
}

func printEmailImportResult(out io.Writer, result map[string]any) {
	if scanned, ok := result["scanned"]; ok {
		fmt.Fprintf(out, "Scanned:  %v\n", scanned)
	}
	if imported, ok := result["imported"]; ok {
		fmt.Fprintf(out, "Imported: %v\n", imported)
	}
	if skipped, ok := result["skipped"]; ok {
		fmt.Fprintf(out, "Skipped:  %v\n", skipped)
	}
	if failed, ok := result["failed"]; ok {
		if v, _ := failed.(float64); v > 0 {
			fmt.Fprintf(out, "Failed:   %v\n", failed)
		}
	}

	// Print routing explain output for dry-run.
	if routes, ok := result["routes"].([]any); ok && len(routes) > 0 {
		fmt.Fprintln(out, "\nRouting decisions:")
		for _, r := range routes {
			if route, ok := r.(map[string]any); ok {
				idx, _ := route["message_index"].(float64)
				ruleID, _ := route["rule_id"].(string)
				pipelineName, _ := route["pipeline"].(string)
				dropped, _ := route["dropped"].(bool)
				explainRaw, _ := route["explain"].([]any)

				status := pipelineName
				if dropped {
					status = "DROPPED"
				}
				fmt.Fprintf(out, "  [%d] rule=%s → %s", int(idx), ruleID, status)
				if len(explainRaw) > 0 {
					parts := make([]string, 0, len(explainRaw))
					for _, e := range explainRaw {
						if s, ok := e.(string); ok {
							parts = append(parts, s)
						}
					}
					fmt.Fprintf(out, " (%s)", strings.Join(parts, ", "))
				}
				fmt.Fprintln(out)
			}
		}
	}
}
