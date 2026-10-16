package watcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Watch modes: how a watched tree's files are parsed. DetectMode picks one
// from the directory layout.
const (
	ModeObsidian = "obsidian"
	ModeLogseq   = "logseq"
	ModeGeneric  = "generic"
)

// contentTypeText is the Request type for plain and markdown text.
const contentTypeText = "text"

// ParseFile reads a single file and returns a Request suitable for ingestion.
// mode controls how the file is parsed: "obsidian", "logseq", or "generic".
// vaultRoot is the root directory of the watch (used to compute relPath as Source).
func ParseFile(path, mode, vaultRoot string) (Request, error) {
	switch mode {
	case ModeObsidian:
		return parseObsidianFile(path, vaultRoot)
	case ModeLogseq:
		return parseLogseqFile(path, vaultRoot)
	default:
		return parseGenericFile(path, vaultRoot)
	}
}

func parseGenericFile(path, vaultRoot string) (Request, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Request{}, fmt.Errorf("read %s: %w", path, err)
	}
	relPath := relativeToVault(path, vaultRoot)
	return Request{
		Content: string(data),
		Type:    detectType(path),
		Source:  "watch:generic:" + relPath,
	}, nil
}

func parseObsidianFile(path, vaultRoot string) (Request, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Request{}, fmt.Errorf("read %s: %w", path, err)
	}
	relPath := relativeToVault(path, vaultRoot)
	return Request{
		Content: string(data),
		Type:    contentTypeText,
		Source:  "watch:obsidian:" + relPath,
	}, nil
}

func parseLogseqFile(path, vaultRoot string) (Request, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Request{}, fmt.Errorf("read %s: %w", path, err)
	}
	relPath := relativeToVault(path, vaultRoot)
	return Request{
		Content: string(data),
		Type:    contentTypeText,
		Source:  "watch:logseq:" + relPath,
	}, nil
}

func relativeToVault(path, vaultRoot string) string {
	rel := strings.TrimPrefix(path, vaultRoot+string(os.PathSeparator))
	return filepath.ToSlash(rel)
}
