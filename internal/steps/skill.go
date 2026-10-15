package steps

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ErrInvalidStep marks a step package that cannot be installed: a bad
// name, archive, SKILL.md or executable.
var ErrInvalidStep = errors.New("invalid step")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidStep, fmt.Sprintf(format, args...))
}

// skillFrontmatter is SKILL.md's YAML frontmatter (ADR-058).
type skillFrontmatter struct {
	Name           string   `yaml:"name"`
	Description    string   `yaml:"description"`
	License        string   `yaml:"license"`
	Version        string   `yaml:"version"`
	Author         string   `yaml:"author"`
	SupportedLangs []string `yaml:"supported_languages"`
	Execution      *struct {
		Type             string         `yaml:"type"`
		Executable       string         `yaml:"executable"`
		ConfigSchema     map[string]any `yaml:"config_schema"`
		MinimumIsolation string         `yaml:"minimum_isolation"`
		Network          bool           `yaml:"network"`
		ContainerImage   string         `yaml:"container_image"`
		Resources        struct {
			MaxMemory string `yaml:"max_memory"`
			MaxCPU    string `yaml:"max_cpu"`
			Timeout   string `yaml:"timeout"`
			MaxOutput string `yaml:"max_output"`
		} `yaml:"resource_requirements"`
	} `yaml:"execution"`
}

// ParseSkill reads SKILL.md content into step metadata and checks the
// execution block: an external step naming an executable by a clean
// relative path, a known isolation level, parseable limits and a
// compilable config schema. It does not look at the files.
func ParseSkill(content []byte) (*storage.StepMetadata, error) {
	front, ok := frontmatter(content)
	if !ok {
		return nil, invalid("SKILL.md has no YAML frontmatter (--- ... ---)")
	}
	var fm skillFrontmatter
	dec := yaml.NewDecoder(bytes.NewReader(front))
	if err := dec.Decode(&fm); err != nil {
		return nil, invalid("SKILL.md frontmatter: %v", err)
	}
	if fm.Execution == nil {
		return nil, invalid("SKILL.md declares no execution block; a step needs execution.type: external and execution.executable")
	}
	x := fm.Execution
	if x.Type != "external" {
		return nil, invalid("SKILL.md execution.type is %q; want \"external\"", x.Type)
	}
	exe, err := cleanRelPath(x.Executable)
	if err != nil {
		return nil, invalid("SKILL.md execution.executable: %v", err)
	}
	switch x.MinimumIsolation {
	case "", storage.StepIsolationProcess, storage.StepIsolationContainer:
	default:
		return nil, invalid("SKILL.md execution.minimum_isolation is %q; want process or container", x.MinimumIsolation)
	}
	exec := &storage.StepExecution{
		Type:             x.Type,
		Executable:       exe,
		ConfigSchema:     x.ConfigSchema,
		MinimumIsolation: x.MinimumIsolation,
		Network:          x.Network,
		ContainerImage:   x.ContainerImage,
		Resources: storage.StepResources{
			MaxMemory: x.Resources.MaxMemory,
			MaxCPU:    x.Resources.MaxCPU,
			Timeout:   x.Resources.Timeout,
			MaxOutput: x.Resources.MaxOutput,
		},
	}
	if exec.MinimumIsolation == storage.StepIsolationContainer && exec.ContainerImage == "" {
		return nil, invalid("SKILL.md requires container isolation but names no execution.container_image")
	}
	if _, err := parseResources(exec.Resources); err != nil {
		return nil, invalid("SKILL.md execution.resource_requirements: %v", err)
	}
	if _, err := compileSchema(exec.ConfigSchema); err != nil {
		return nil, invalid("SKILL.md execution.config_schema: %v", err)
	}
	return &storage.StepMetadata{
		Name:           fm.Name,
		Description:    fm.Description,
		License:        fm.License,
		Version:        fm.Version,
		Author:         fm.Author,
		SupportedLangs: fm.SupportedLangs,
		Execution:      exec,
	}, nil
}

// frontmatter returns the YAML between the leading --- lines.
func frontmatter(content []byte) ([]byte, bool) {
	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, false
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return []byte(strings.Join(lines[1:i], "\n")), true
		}
	}
	return nil, false
}

// cleanRelPath accepts a slash-separated path that stays inside its root.
func cleanRelPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("empty path")
	}
	if strings.Contains(p, "\\") || path.IsAbs(p) {
		return "", fmt.Errorf("%q must be relative to the step directory", p)
	}
	c := path.Clean(p)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return "", fmt.Errorf("%q leaves the step directory", p)
	}
	return c, nil
}

// limits are a step's parsed resource requirements; zero means unset.
type limits struct {
	MaxMemory uint64
	MaxCPU    float64 // CPUs, container isolation only
	Timeout   time.Duration
	MaxOutput int64
}

func parseResources(r storage.StepResources) (limits, error) {
	var l limits
	if r.MaxMemory != "" {
		n, err := humanize.ParseBytes(r.MaxMemory)
		if err != nil || n == 0 {
			return l, fmt.Errorf("max_memory %q: want a size such as 256MB", r.MaxMemory)
		}
		l.MaxMemory = n
	}
	if r.MaxOutput != "" {
		n, err := humanize.ParseBytes(r.MaxOutput)
		if err != nil || n == 0 || n > 1<<40 {
			return l, fmt.Errorf("max_output %q: want a size such as 64MB", r.MaxOutput)
		}
		l.MaxOutput = int64(n) // #nosec G115 -- bounded above
	}
	if r.Timeout != "" {
		d, err := time.ParseDuration(r.Timeout)
		if err != nil || d <= 0 {
			return l, fmt.Errorf("timeout %q: want a duration such as 30s", r.Timeout)
		}
		l.Timeout = d
	}
	if r.MaxCPU != "" {
		cpu, err := parseCPU(r.MaxCPU)
		if err != nil {
			return l, err
		}
		l.MaxCPU = cpu
	}
	return l, nil
}

// parseCPU reads "50%" (half a CPU) or "1.5" (CPUs).
func parseCPU(s string) (float64, error) {
	var v float64
	if pct, ok := strings.CutSuffix(s, "%"); ok {
		if _, err := fmt.Sscanf(pct, "%g", &v); err != nil || v <= 0 {
			return 0, fmt.Errorf("max_cpu %q: want a share such as 50%% or a CPU count such as 0.5", s)
		}
		return v / 100, nil
	}
	if _, err := fmt.Sscanf(s, "%g", &v); err != nil || v <= 0 {
		return 0, fmt.Errorf("max_cpu %q: want a share such as 50%% or a CPU count such as 0.5", s)
	}
	return v, nil
}

// compileSchema compiles a step's config_schema; nil when it has none.
func compileSchema(schema map[string]any) (*jsonschema.Schema, error) {
	if len(schema) == 0 {
		return nil, nil
	}
	// Round-trip through JSON: YAML maps hold types the compiler rejects.
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("step-config.json", doc); err != nil {
		return nil, err
	}
	return c.Compile("step-config.json")
}
