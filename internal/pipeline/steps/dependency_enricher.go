package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// DependencyEnricher detects package dependencies from a repo knowledge object
// and enqueues each dependency as a new ingestion item.
//
// Reads:
//   - draft.Metadata["dep_files"]    map[string]any  filename → content (optional)
//   - draft.Source                   string          repo URL (used for context)
//   - draft.RawContent               string          fallback: parsed if dep_files absent
//
// Supported manifest formats:
//   - package.json  (npm)
//   - go.mod        (Go modules)
//   - requirements.txt (Python pip)
//   - Gemfile       (Ruby bundler)
//
// Produces:
//   - draft.Metadata["items_to_enqueue"]   []map[string]any
//   - draft.Metadata["deps_enqueued"]      int
type DependencyEnricher struct {
	pipeline.BaseContract
}

// NewDependencyEnricher creates a DependencyEnricher.
func NewDependencyEnricher() *DependencyEnricher {
	return &DependencyEnricher{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"RawContent"},
			Produces: []string{"Metadata"},
		}),
	}
}

func (s *DependencyEnricher) Name() string { return "dependency_enricher" }

func (s *DependencyEnricher) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	deps, err := s.extractDeps(draft)
	if err != nil {
		return nil, fmt.Errorf("dependency_enricher: %w", err)
	}

	existing, _ := draft.Metadata["items_to_enqueue"].([]map[string]any)
	pending := make([]map[string]any, 0, len(existing)+len(deps))
	pending = append(pending, existing...)

	for _, dep := range deps {
		pending = append(pending, map[string]any{
			"content":  dep.URL,
			"source":   draft.Source,
			"pipeline": dep.Pipeline,
		})
	}

	draft.Metadata["items_to_enqueue"] = pending
	draft.Metadata["deps_enqueued"] = len(deps)

	// Emit canonical graph nodes: one NodeTypeArtifact per detected dependency.
	// Skip when draft.ID is empty (pre-ID pipeline drafts).
	if draft.ID != "" && len(deps) > 0 {
		if draft.Graph == nil {
			draft.Graph = &pluginapi.ObjectGraph{}
		}
		for i, dep := range deps {
			nodeID := pluginapi.NewNodeID(draft.ID, pluginapi.NodeTypeArtifact, i)
			if draft.Graph.FindNode(nodeID) != nil {
				continue
			}
			draft.Graph.Nodes = append(draft.Graph.Nodes, pluginapi.GraphNode{
				ID:       nodeID,
				NodeType: pluginapi.NodeTypeArtifact,
				Label:    dep.URL,
				Order:    i,
				Metadata: map[string]any{
					"pipeline": dep.Pipeline,
				},
			})
			draft.Graph.Edges = append(draft.Graph.Edges, pluginapi.GraphEdge{
				ID:       fmt.Sprintf("%s->%s", draft.ID, nodeID),
				FromID:   draft.ID,
				ToID:     nodeID,
				EdgeType: pluginapi.EdgeTypeContains,
			})
		}
	}

	return draft, nil
}

// depEntry holds a resolved dependency URL and its preferred ingestion pipeline.
type depEntry struct {
	URL      string
	Pipeline string
}

// extractDeps resolves dependencies from dep_files metadata or RawContent fallback.
func (s *DependencyEnricher) extractDeps(draft *storage.KnowledgeObject) ([]depEntry, error) {
	if raw, ok := draft.Metadata["dep_files"]; ok {
		files, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("dep_files has unexpected type %T", raw)
		}
		return parseDepFiles(files)
	}
	// Fallback: try parsing RawContent as a known manifest format.
	return inferFromRawContent(draft.RawContent)
}

// parseDepFiles iterates over filename→content entries and dispatches to per-format parsers.
func parseDepFiles(files map[string]any) ([]depEntry, error) {
	var deps []depEntry
	for name, v := range files {
		content, ok := v.(string)
		if !ok {
			continue
		}
		parsed, err := parseManifest(name, content)
		if err != nil {
			return nil, fmt.Errorf("parse %q: %w", name, err)
		}
		deps = append(deps, parsed...)
	}
	return deps, nil
}

// parseManifest dispatches to the right parser based on filename.
func parseManifest(filename, content string) ([]depEntry, error) {
	base := strings.ToLower(baseName(filename))
	switch base {
	case "package.json":
		return parsePackageJSON(content)
	case "go.mod":
		return parseGoMod(content)
	case "requirements.txt":
		return parseRequirementsTxt(content)
	case "gemfile":
		return parseGemfile(content)
	default:
		return nil, nil
	}
}

// baseName returns the base name of a file path (last path component).
func baseName(p string) string {
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// inferFromRawContent tries to auto-detect a manifest format from the content.
func inferFromRawContent(content string) ([]depEntry, error) {
	trimmed := strings.TrimSpace(content)
	// package.json: starts with { and contains "dependencies"
	if strings.HasPrefix(trimmed, "{") && strings.Contains(trimmed, `"dependencies"`) {
		return parsePackageJSON(trimmed)
	}
	// go.mod: starts with "module "
	if strings.HasPrefix(trimmed, "module ") {
		return parseGoMod(trimmed)
	}
	// requirements.txt: no structured header, line-by-line packages
	if looksLikeRequirements(trimmed) {
		return parseRequirementsTxt(trimmed)
	}
	return nil, nil
}

var requirementsLineRe = regexp.MustCompile(`(?m)^[a-zA-Z][a-zA-Z0-9_.-]+`)

func looksLikeRequirements(content string) bool {
	lines := strings.Split(content, "\n")
	matches := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if requirementsLineRe.MatchString(line) {
			matches++
		}
	}
	return matches >= 2 && matches > len(lines)/3
}

// parsePackageJSON parses npm/yarn package.json for dependencies.
func parsePackageJSON(content string) ([]depEntry, error) {
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal([]byte(content), &pkg); err != nil {
		return nil, fmt.Errorf("package.json: %w", err)
	}
	var deps []depEntry
	for name := range pkg.Dependencies {
		deps = append(deps, npmEntry(name))
	}
	for name := range pkg.DevDependencies {
		deps = append(deps, npmEntry(name))
	}
	return deps, nil
}

func npmEntry(pkg string) depEntry {
	return depEntry{
		URL:      "https://www.npmjs.com/package/" + pkg,
		Pipeline: "url.generic",
	}
}

// goModInlineRe matches "require <module> <version>" single-line require directives.
var goModInlineRe = regexp.MustCompile(`(?m)^\s*require\s+([\w.\-/]+)\s+v[^\s]+`)

// goModBlockRe matches "<module> <version>" lines inside a require(...) block.
var goModBlockRe = regexp.MustCompile(`(?m)^\s+([\w.\-/]+)\s+v[^\s]+`)

// parseGoMod parses go.mod for require directives.
func parseGoMod(content string) ([]depEntry, error) {
	seen := make(map[string]bool)
	var deps []depEntry

	addMod := func(mod string) {
		mod = strings.TrimSpace(mod)
		if mod == "" || seen[mod] {
			return
		}
		seen[mod] = true
		deps = append(deps, goEntry(mod))
	}

	// Inline requires: "require module v1.0.0"
	for _, m := range goModInlineRe.FindAllStringSubmatch(content, -1) {
		if len(m) >= 2 {
			addMod(m[1])
		}
	}

	// Block requires: lines inside "require ( ... )" indented with a tab/space.
	for _, m := range goModBlockRe.FindAllStringSubmatch(content, -1) {
		if len(m) >= 2 {
			addMod(m[1])
		}
	}

	return deps, nil
}

func goEntry(module string) depEntry {
	return depEntry{
		URL:      "https://pkg.go.dev/" + module,
		Pipeline: "url.generic",
	}
}

// requirementsEntryRe matches "package[>=<! ...]" lines from requirements.txt.
var requirementsEntryRe = regexp.MustCompile(`^([a-zA-Z][a-zA-Z0-9_.-]+)`)

// parseRequirementsTxt parses pip requirements.txt.
func parseRequirementsTxt(content string) ([]depEntry, error) {
	var deps []depEntry
	seen := make(map[string]bool)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		m := requirementsEntryRe.FindStringSubmatch(line)
		if len(m) < 2 {
			continue
		}
		pkg := m[1]
		if seen[pkg] {
			continue
		}
		seen[pkg] = true
		deps = append(deps, pypiEntry(pkg))
	}
	return deps, nil
}

func pypiEntry(pkg string) depEntry {
	return depEntry{
		URL:      "https://pypi.org/project/" + pkg + "/",
		Pipeline: "url.generic",
	}
}

// gemfileEntryRe matches 'gem "name"' or "gem 'name'" lines.
var gemfileEntryRe = regexp.MustCompile(`(?m)^\s*gem\s+['"]([^'"]+)['"]`)

// parseGemfile parses a Ruby Gemfile.
func parseGemfile(content string) ([]depEntry, error) {
	matches := gemfileEntryRe.FindAllStringSubmatch(content, -1)
	var deps []depEntry
	seen := make(map[string]bool)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		gem := m[1]
		if seen[gem] {
			continue
		}
		seen[gem] = true
		deps = append(deps, rubygemsEntry(gem))
	}
	return deps, nil
}

func rubygemsEntry(gem string) depEntry {
	return depEntry{
		URL:      "https://rubygems.org/gems/" + gem,
		Pipeline: "url.generic",
	}
}
