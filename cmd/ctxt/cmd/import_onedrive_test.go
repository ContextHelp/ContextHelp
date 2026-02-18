package cmd

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	onedriveimporter "github.com/ideacrafterslabs/ctxt/internal/importer/onedrive"
)

type fakeOneDriveClient struct {
	listDriveFolderItems func(ctx context.Context, driveID, folderPath string, maxItems int) ([]onedriveimporter.Item, error)
	getItem              func(ctx context.Context, driveID, itemID string) (onedriveimporter.Item, error)
	listSharedWithMe     func(ctx context.Context, maxItems int) ([]onedriveimporter.Item, error)
}

func (f *fakeOneDriveClient) ListDriveFolderItems(ctx context.Context, driveID, folderPath string, maxItems int) ([]onedriveimporter.Item, error) {
	if f.listDriveFolderItems == nil {
		return nil, nil
	}
	return f.listDriveFolderItems(ctx, driveID, folderPath, maxItems)
}

func (f *fakeOneDriveClient) GetItem(ctx context.Context, driveID, itemID string) (onedriveimporter.Item, error) {
	if f.getItem == nil {
		return onedriveimporter.Item{}, fmt.Errorf("unexpected GetItem call for %s/%s", driveID, itemID)
	}
	return f.getItem(ctx, driveID, itemID)
}

func (f *fakeOneDriveClient) ListSharedWithMe(ctx context.Context, maxItems int) ([]onedriveimporter.Item, error) {
	if f.listSharedWithMe == nil {
		return nil, nil
	}
	return f.listSharedWithMe(ctx, maxItems)
}

func TestImportOneDriveRequiresToken(t *testing.T) {
	t.Setenv("ONEDRIVE_TOKEN", "")

	_, err := executeCommand("import", "onedrive", "--drive-id", "b!drive123", "--dry-run")
	if err == nil {
		t.Fatal("expected missing token to fail")
	}
}

func TestImportOneDriveRequiresScope(t *testing.T) {
	t.Setenv("ONEDRIVE_TOKEN", "token-123")

	_, err := executeCommand("import", "onedrive", "--dry-run")
	if err == nil {
		t.Fatal("expected missing scope selectors to fail")
	}
}

func TestImportOneDriveInvalidDriveFolderRef(t *testing.T) {
	t.Setenv("ONEDRIVE_TOKEN", "token-123")

	_, err := executeCommand("import", "onedrive", "--drive-folder", "badref", "--dry-run")
	if err == nil {
		t.Fatal("expected invalid --drive-folder to fail")
	}
}

func TestImportOneDriveDryRunSelectiveScope(t *testing.T) {
	t.Setenv("ONEDRIVE_TOKEN", "token-123")

	origFactory := newOneDriveClient
	t.Cleanup(func() { newOneDriveClient = origFactory })

	cutoff := mustParseRFC3339(t, "2026-01-01T00:00:00Z")

	newOneDriveClient = func(token, baseURL string) oneDriveClient {
		return &fakeOneDriveClient{
			listDriveFolderItems: func(ctx context.Context, driveID, folderPath string, maxItems int) ([]onedriveimporter.Item, error) {
				if folderPath == "/" {
					return []onedriveimporter.Item{
						{
							DriveID:          "b!driveA",
							ID:               "id-1",
							Name:             "report.pdf",
							Path:             "/report.pdf",
							WebURL:           "https://example.com/report",
							LastModifiedTime: mustParseRFC3339(t, "2026-02-10T09:00:00Z"),
							IsFile:           true,
						},
						{
							DriveID:          "b!driveA",
							ID:               "id-2",
							Name:             "notes.txt",
							Path:             "/notes.txt",
							WebURL:           "https://example.com/notes",
							LastModifiedTime: mustParseRFC3339(t, "2026-02-11T09:00:00Z"),
							IsFile:           true,
						},
					}, nil
				}
				return []onedriveimporter.Item{
					{
						DriveID:          "b!driveA",
						ID:               "id-3",
						Name:             "old.pdf",
						Path:             "/Projects/old.pdf",
						WebURL:           "https://example.com/old",
						LastModifiedTime: mustParseRFC3339(t, "2025-12-10T09:00:00Z"),
						IsFile:           true,
					},
				}, nil
			},
			listSharedWithMe: func(ctx context.Context, maxItems int) ([]onedriveimporter.Item, error) {
				return []onedriveimporter.Item{
					{
						DriveID:          "b!driveA",
						ID:               "id-1", // duplicate, should dedup
						Name:             "report.pdf",
						Path:             "/Shared/report.pdf",
						WebURL:           "https://example.com/report",
						LastModifiedTime: mustParseRFC3339(t, "2026-02-10T09:00:00Z"),
						IsFile:           true,
					},
					{
						DriveID:          "b!driveShared",
						ID:               "id-4",
						Name:             "shared.pdf",
						Path:             "/shared.pdf",
						WebURL:           "https://example.com/shared",
						LastModifiedTime: mustParseRFC3339(t, "2026-02-12T09:00:00Z"),
						IsFile:           true,
					},
				}, nil
			},
		}
	}

	out, err := executeCommand(
		"import", "onedrive",
		"--drive-id", "b!driveA",
		"--drive-folder", "b!driveA:/Projects",
		"--shared-with-me",
		"--since", cutoff.Format(time.RFC3339),
		"--include-ext", ".pdf",
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("import onedrive dry-run should succeed: %v", err)
	}

	if !strings.Contains(out, "OneDrive items selected: 2") {
		t.Fatalf("expected selected count in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Scanned: 5, Skipped: 2") {
		t.Fatalf("expected scanned/skipped summary, got:\n%s", out)
	}
	if !strings.Contains(out, "shared.pdf") {
		t.Fatalf("expected shared item in preview, got:\n%s", out)
	}
}

func TestImportOneDriveEnqueue(t *testing.T) {
	t.Setenv("ONEDRIVE_TOKEN", "token-123")

	origFactory := newOneDriveClient
	origEnqueue := enqueueOneDriveContent
	t.Cleanup(func() {
		newOneDriveClient = origFactory
		enqueueOneDriveContent = origEnqueue
	})

	newOneDriveClient = func(token, baseURL string) oneDriveClient {
		return &fakeOneDriveClient{
			listDriveFolderItems: func(ctx context.Context, driveID, folderPath string, maxItems int) ([]onedriveimporter.Item, error) {
				return []onedriveimporter.Item{
					{
						DriveID:          "b!driveA",
						ID:               "id-1",
						Name:             "report.pdf",
						Path:             "/report.pdf",
						WebURL:           "https://example.com/report",
						LastModifiedTime: mustParseRFC3339(t, "2026-02-10T09:00:00Z"),
						IsFile:           true,
					},
				}, nil
			},
		}
	}

	var enqueueCalls int
	enqueueOneDriveContent = func(serverURL, content, contentType, pipelineName, source string) (string, error) {
		enqueueCalls++
		if contentType != "text" {
			return "", fmt.Errorf("unexpected content type: %s", contentType)
		}
		if !strings.Contains(content, "Microsoft OneDrive/SharePoint") {
			return "", fmt.Errorf("expected onedrive payload content")
		}
		return "job-123", nil
	}

	out, err := executeCommand("import", "onedrive", "--drive-id", "b!driveA")
	if err != nil {
		t.Fatalf("import onedrive enqueue should succeed: %v", err)
	}

	if enqueueCalls != 1 {
		t.Fatalf("expected exactly one enqueue call, got %d", enqueueCalls)
	}
	if !strings.Contains(out, "OneDrive items processed: 1") {
		t.Fatalf("expected processed summary, got:\n%s", out)
	}
	if !strings.Contains(out, "Jobs enqueued: 1") {
		t.Fatalf("expected enqueue summary, got:\n%s", out)
	}
}

func mustParseRFC3339(t *testing.T, ts string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t.Fatalf("parse RFC3339 %q: %v", ts, err)
	}
	return parsed
}
