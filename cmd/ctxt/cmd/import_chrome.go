package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	gohttp "net/http"
	"strings"

	bookmarksimporter "github.com/ideacrafterslabs/ctxt/internal/importer/bookmarks"
	"github.com/spf13/cobra"
)

var importChromeCmd = &cobra.Command{
	Use:   "chrome",
	Short: "Import Chrome bookmarks export",
	Long: `Import bookmarks from a Chrome bookmarks HTML export file.

Examples:
  # Dry-run parse and preview bookmarks
  ctxt import chrome --file ./bookmarks.html --dry-run

  # Enqueue bookmark URLs for ingestion
  ctxt import chrome --file ./bookmarks.html --server http://localhost:8080`,
	RunE: runImportChrome,
}

func init() {
	importCmd.AddCommand(importChromeCmd)

	importChromeCmd.Flags().String("file", "", "path to Chrome bookmarks HTML export")
	importChromeCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	importChromeCmd.Flags().String("pipeline", "", "pipeline override for enqueued jobs")
	importChromeCmd.Flags().Int("max-items", 0, "maximum number of bookmarks to import (0 = all)")
	importChromeCmd.Flags().Bool("dry-run", false, "parse and preview without enqueueing jobs")

	importChromeCmd.MarkFlagRequired("file")
}

func runImportChrome(cmd *cobra.Command, args []string) error {
	file, _ := cmd.Flags().GetString("file")
	serverURL, _ := cmd.Flags().GetString("server")
	pipelineName, _ := cmd.Flags().GetString("pipeline")
	maxItems, _ := cmd.Flags().GetInt("max-items")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	serverURL = strings.TrimRight(serverURL, "/")

	bookmarks, err := bookmarksimporter.ParseBookmarksFile(file)
	if err != nil {
		return err
	}
	if len(bookmarks) == 0 {
		return fmt.Errorf("no bookmarks found in %s", file)
	}

	if maxItems > 0 && len(bookmarks) > maxItems {
		bookmarks = bookmarks[:maxItems]
	}

	if dryRun {
		const previewLimit = 20
		fmt.Printf("Chrome bookmarks parsed: %d (dry-run)\n", len(bookmarks))

		preview := len(bookmarks)
		if preview > previewLimit {
			preview = previewLimit
		}

		for i := 0; i < preview; i++ {
			b := bookmarks[i]
			if b.FolderPath == "" {
				fmt.Printf("%d. %s -> %s\n", i+1, b.Title, b.URL)
			} else {
				fmt.Printf("%d. [%s] %s -> %s\n", i+1, b.FolderPath, b.Title, b.URL)
			}
		}

		if len(bookmarks) > previewLimit {
			fmt.Printf("... and %d more bookmarks\n", len(bookmarks)-previewLimit)
		}
		return nil
	}

	var (
		success  int
		failed   int
		firstErr error
	)

	for _, b := range bookmarks {
		_, err := enqueueBookmark(serverURL, b.URL, pipelineName, "import:chrome")
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		success++
	}

	fmt.Printf("Chrome bookmarks processed: %d\n", len(bookmarks))
	fmt.Printf("Jobs enqueued: %d\n", success)
	if failed > 0 {
		fmt.Printf("Jobs failed: %d\n", failed)
	}

	if firstErr != nil {
		return fmt.Errorf("failed to enqueue %d bookmarks (first error: %w)", failed, firstErr)
	}

	return nil
}

type enqueueRequest struct {
	Content  string `json:"content"`
	Type     string `json:"type"`
	Pipeline string `json:"pipeline,omitempty"`
	Source   string `json:"source,omitempty"`
}

type enqueueResponse struct {
	JobID string `json:"job_id"`
}

func enqueueBookmark(serverURL, url, pipelineName, source string) (string, error) {
	return enqueueContent(serverURL, url, "url", pipelineName, source)
}

func enqueueContent(serverURL, content, contentType, pipelineName, source string) (string, error) {
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

		// Backward compatibility fallback for older servers.
		if statusCode == gohttp.StatusNotFound && i == 0 {
			continue
		}
		if statusCode != gohttp.StatusAccepted && statusCode != gohttp.StatusOK {
			return "", fmt.Errorf("dpkms returned %d: %s", statusCode, strings.TrimSpace(respBody))
		}

		return jobID, nil
	}

	return "", fmt.Errorf("enqueue failed: endpoint unavailable")
}

func postEnqueueRequest(url string, body []byte) (jobID string, statusCode int, respBody string, err error) {
	resp, err := gohttp.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", 0, "", fmt.Errorf("request to dpkms: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, "", fmt.Errorf("read response: %w", err)
	}

	var parsed enqueueResponse
	if err := json.Unmarshal(data, &parsed); err == nil {
		return parsed.JobID, resp.StatusCode, string(data), nil
	}

	return "", resp.StatusCode, string(data), nil
}
