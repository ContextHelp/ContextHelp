package watcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// ParseFile reads a single file and returns an AnalyzeRequest suitable for ingestion.
// mode controls how the file is parsed: "obsidian", "logseq", or "generic".
// vaultRoot is the root directory of the watch (used to compute relPath as Source).
func ParseFile(path, mode, vaultRoot string) (service.AnalyzeRequest, error) {
	switch mode {
	case "obsidian":
		return parseObsidianFile(path, vaultRoot)
	case "logseq":
		return parseLogseqFile(path, vaultRoot)
	default:
		return parseGenericFile(path, vaultRoot)
	}
}

func parseGenericFile(path, vaultRoot string) (service.AnalyzeRequest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return service.AnalyzeRequest{}, fmt.Errorf("read %s: %w", path, err)
	}
	relPath := relativeToVault(path, vaultRoot)
	return service.AnalyzeRequest{
		Content: string(data),
		Type:    detectType(path),
		Source:  "watch:generic:" + relPath,
	}, nil
}

func parseObsidianFile(path, vaultRoot string) (service.AnalyzeRequest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return service.AnalyzeRequest{}, fmt.Errorf("read %s: %w", path, err)
	}
	relPath := relativeToVault(path, vaultRoot)
	return service.AnalyzeRequest{
		Content: string(data),
		Type:    "text",
		Source:  "watch:obsidian:" + relPath,
	}, nil
}

func parseLogseqFile(path, vaultRoot string) (service.AnalyzeRequest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return service.AnalyzeRequest{}, fmt.Errorf("read %s: %w", path, err)
	}
	relPath := relativeToVault(path, vaultRoot)
	return service.AnalyzeRequest{
		Content: string(data),
		Type:    "text",
		Source:  "watch:logseq:" + relPath,
	}, nil
}

func relativeToVault(path, vaultRoot string) string {
	rel := strings.TrimPrefix(path, vaultRoot+string(os.PathSeparator))
	return filepath.ToSlash(rel)
}
