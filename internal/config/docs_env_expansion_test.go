package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Historical and planning records keep the text they were written with.
var configDocsSkipped = []string{
	"docs/plans", "docs/superpowers", "docs/sprints", "docs/analysis",
	"docs/release-notes", "docs/decisions", "docs/stories", "docs/public/node_modules",
}

// Load does not expand ${VAR}: a config value written that way is the
// literal string, so a token example would set the token "${...}". No
// config example, in the shipped config/ files or in a yaml block of the
// docs, may show one. Compose files (a block with services:) interpolate
// for real and are exempt.
func TestConfigExamples_ShowNoEnvExpansion(t *testing.T) {
	root := filepath.Join("..", "..")

	examples, err := filepath.Glob(filepath.Join(root, "config", "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, examples)
	for _, f := range examples {
		data, err := os.ReadFile(f)
		require.NoError(t, err)
		for i, line := range strings.Split(string(data), "\n") {
			assert.NotRegexp(t, shellEnvRef, line, "%s:%d: config does not expand env vars", f, i+1)
		}
	}

	docs := filepath.Join(root, "docs")
	err = filepath.WalkDir(docs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			for _, skip := range configDocsSkipped {
				if rel == skip {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, b := range yamlBlocks(string(data)) {
			if strings.Contains(b.text, "services:") {
				continue
			}
			for i, line := range strings.Split(b.text, "\n") {
				assert.NotRegexp(t, shellEnvRef, line, "%s:%d: config does not expand env vars", rel, b.line+i)
			}
		}
		return nil
	})
	require.NoError(t, err)
}

type mdBlock struct {
	line int // 1-based line of the block's first content line
	text string
}

// yamlBlocks returns the ```yaml / ```yml fenced blocks of a markdown file.
func yamlBlocks(md string) []mdBlock {
	var blocks []mdBlock
	var cur []string
	start, in := 0, false
	for i, line := range strings.Split(md, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case !in && (strings.HasPrefix(trimmed, "```yaml") || strings.HasPrefix(trimmed, "```yml")):
			in, start, cur = true, i+2, nil
		case in && strings.HasPrefix(trimmed, "```"):
			blocks = append(blocks, mdBlock{line: start, text: strings.Join(cur, "\n")})
			in = false
		case in:
			cur = append(cur, line)
		}
	}
	return blocks
}
