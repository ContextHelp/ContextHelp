package steps

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Runners are the runners installed steps use, by isolation level.
type Runners struct {
	Process   Runner
	Container Runner
}

// DefaultRunners are the real runners.
func DefaultRunners() Runners {
	return Runners{Process: ProcessRunner{}, Container: ContainerRunner{}}
}

// Catalog is the set of installed steps pipelines may insert. It
// satisfies builtins.ExternalSteps.
type Catalog struct {
	steps   map[string]*storage.RegisteredStep
	runners Runners
	// WorkRoot is where runs make their working directories; empty
	// means os.TempDir().
	WorkRoot string
}

// NewCatalog is a catalog over installed steps.
func NewCatalog(installed []*storage.RegisteredStep, runners Runners) *Catalog {
	c := &Catalog{steps: map[string]*storage.RegisteredStep{}, runners: runners}
	for _, s := range installed {
		if s != nil && s.Metadata != nil && s.Metadata.Execution != nil {
			c.steps[s.Name] = s
		}
	}
	return c
}

// LoadCatalog reads the installed steps from the steps table.
func LoadCatalog(ctx context.Context, store storage.StorageDriver, runners Runners) (*Catalog, error) {
	installed, _, err := store.Steps().List(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("list installed steps: %w", err)
	}
	return NewCatalog(installed, runners), nil
}

// Has reports whether a runnable step of that name is installed.
func (c *Catalog) Has(name string) bool {
	if c == nil {
		return false
	}
	_, ok := c.steps[name]
	return ok
}

// Names lists the installed step names.
func (c *Catalog) Names() []string {
	out := make([]string, 0, len(c.steps))
	for n := range c.steps {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Step builds the installed step ins names for one pipeline: its
// isolation (the strictest of SKILL.md's minimum_isolation, the
// registry's trust_level and ins.Isolation), limits, failure policy and
// config, checked against the step's config_schema.
func (c *Catalog) Step(pipelineName string, ins config.StepInsert) (pipeline.PipelineStep, error) {
	rs, ok := c.steps[ins.Step]
	if !ok {
		return nil, fmt.Errorf("step %q is not installed", ins.Step)
	}
	x := rs.Metadata.Execution
	lim, err := parseResources(x.Resources)
	if err != nil {
		return nil, fmt.Errorf("step %q: %w", ins.Step, err)
	}
	if ins.Timeout > 0 {
		lim.Timeout = ins.Timeout
	}
	if lim.Timeout <= 0 {
		lim.Timeout = defaultTimeout
	}
	if lim.MaxOutput <= 0 {
		lim.MaxOutput = defaultOutput
	}

	isolation := EffectiveIsolation(x, ins.Isolation)
	runner := c.runners.Process
	if isolation == storage.StepIsolationContainer {
		runner = c.runners.Container
		if x.ContainerImage == "" {
			return nil, fmt.Errorf("step %q runs under container isolation but its SKILL.md names no execution.container_image", ins.Step)
		}
	}
	if runner == nil {
		return nil, fmt.Errorf("step %q: no %s runner", ins.Step, isolation)
	}

	cfgJSON := []byte("{}")
	if len(ins.Config) > 0 {
		if cfgJSON, err = json.Marshal(ins.Config); err != nil {
			return nil, fmt.Errorf("step %q: config: %w", ins.Step, err)
		}
	}
	sch, err := compileSchema(x.ConfigSchema)
	if err != nil {
		return nil, fmt.Errorf("step %q: config_schema: %w", ins.Step, err)
	}
	if sch != nil {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(cfgJSON))
		if err != nil {
			return nil, fmt.Errorf("step %q: config: %w", ins.Step, err)
		}
		if err := sch.Validate(doc); err != nil {
			return nil, fmt.Errorf("pipelines.overrides.%s: step %q: config does not match its config_schema: %w", pipelineName, ins.Step, err)
		}
	}

	return &ExternalStep{
		BaseContract:  pipeline.NewBaseContract(pipeline.StepContract{Requires: []string{"RawContent"}}),
		name:          ins.Step,
		pipelineName:  pipelineName,
		dir:           rs.Path,
		exec:          *x,
		isolation:     isolation,
		limits:        lim,
		config:        cfgJSON,
		continueOnErr: ins.OnError == config.StepOnErrorContinue,
		runner:        runner,
		workRoot:      c.WorkRoot,
	}, nil
}

// EffectiveIsolation is the strictest of the step's minimum_isolation,
// what its registry's trust_level requires (sandboxed: container) and
// the operator's request.
func EffectiveIsolation(x *storage.StepExecution, requested string) string {
	if x.MinimumIsolation == storage.StepIsolationContainer ||
		x.TrustLevel == string(config.RegistryTrustLevelSandboxed) ||
		requested == storage.StepIsolationContainer {
		return storage.StepIsolationContainer
	}
	return storage.StepIsolationProcess
}

// ExternalStep is an installed step: each Run sends the object to the
// step's executable and takes back what it may change (ADR-058).
type ExternalStep struct {
	pipeline.BaseContract
	name          string
	pipelineName  string
	dir           string
	exec          storage.StepExecution
	isolation     string
	limits        limits
	config        []byte
	continueOnErr bool
	runner        Runner
	workRoot      string
}

// Name is the installed step's name.
func (s *ExternalStep) Name() string { return s.name }

// Isolation is the level the step runs under.
func (s *ExternalStep) Isolation() string { return s.isolation }

// Run runs the step on draft. A failure fails the object (nothing is
// stored) unless the pipeline set on_error: continue for this step, in
// which case it is logged and draft passes on unchanged.
func (s *ExternalStep) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	out, err := s.run(ctx, draft)
	if err == nil {
		return out, nil
	}
	if s.continueOnErr && ctx.Err() == nil {
		slog.Warn("step failed; on_error: continue passes the object on unchanged",
			"step", s.name, "pipeline", s.pipelineName, "err", err)
		return draft, nil
	}
	return nil, err
}

func (s *ExternalStep) run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	exe := filepath.Join(s.dir, filepath.FromSlash(s.exec.Executable))
	if err := verifyDigest(exe, s.exec.ExecutableSHA256); err != nil {
		return nil, pipeline.Permanent(fmt.Errorf("%s: %w", s.name, err))
	}
	in, err := json.Marshal(draft)
	if err != nil {
		return nil, pipeline.Permanent(fmt.Errorf("%s: encode object: %w", s.name, err))
	}
	inv := Invocation{
		StepDir:    s.dir,
		Executable: s.exec.Executable,
		Image:      s.exec.ContainerImage,
		Env:        s.protocolEnv(),
		Config:     s.config,
		Stdin:      in,
		Timeout:    s.limits.Timeout,
		MaxOutput:  s.limits.MaxOutput,
		Limits:     s.limits,
		Network:    s.exec.Network && s.exec.TrustLevel != string(config.RegistryTrustLevelSandboxed),
		WorkRoot:   s.workRoot,
	}
	res, runErr := s.runner.Run(ctx, inv)
	if len(res.StderrTail) > 0 {
		slog.Debug("step stderr", "step", s.name, "stderr", string(res.StderrTail))
	}

	var exitErr *ExitError
	switch {
	case runErr == nil:
	case errors.As(runErr, &exitErr):
		// A step may report a structured error and exit non-zero.
		if r, perr := parseResult(res.Stdout); perr == nil && r.Error != nil {
			return nil, r.Error.asError(s.name)
		}
		return nil, fmt.Errorf("%s: %w", s.name, runErr)
	case errors.Is(runErr, ErrOutputTooLarge):
		return nil, pipeline.Permanent(fmt.Errorf("%s: %w (%d bytes)", s.name, runErr, s.limits.MaxOutput))
	case errors.Is(runErr, ErrIsolationUnavailable):
		return nil, pipeline.Permanent(fmt.Errorf("%s: %w", s.name, runErr))
	default:
		// Timeouts and spawn failures may pass on a retry.
		return nil, fmt.Errorf("%s: %w", s.name, runErr)
	}

	r, err := parseResult(res.Stdout)
	if err != nil {
		return nil, pipeline.Permanent(fmt.Errorf("%s: %w", s.name, err))
	}
	if r.Error != nil {
		return nil, r.Error.asError(s.name)
	}
	if r.Object == nil {
		return nil, pipeline.Permanent(fmt.Errorf("%s: invalid output: no object and no error", s.name))
	}
	merged, err := MergeStepOutput(draft, r.Object)
	if err != nil {
		return nil, pipeline.Permanent(fmt.Errorf("%s: %w", s.name, err))
	}
	return merged, nil
}

func (s *ExternalStep) protocolEnv() map[string]string {
	env := map[string]string{
		"SANDBOX_ENABLED":         "true",
		"SANDBOX_ISOLATION_LEVEL": s.isolation,
		"SANDBOX_TIMEOUT":         s.limits.Timeout.String(),
		"SANDBOX_NETWORK":         fmt.Sprint(s.exec.Network && s.exec.TrustLevel != string(config.RegistryTrustLevelSandboxed)),
		"DPKMS_STEP_NAME":         s.name,
		"DPKMS_PIPELINE_NAME":     s.pipelineName,
	}
	if s.exec.Resources.MaxMemory != "" {
		env["SANDBOX_MAX_MEMORY"] = s.exec.Resources.MaxMemory
	}
	if s.exec.Resources.MaxCPU != "" {
		env["SANDBOX_MAX_CPU"] = s.exec.Resources.MaxCPU
	}
	return env
}

// verifyDigest checks the executable still hashes to what install
// recorded.
func verifyDigest(path, want string) error {
	if want == "" {
		return errors.New("no recorded executable digest; reinstall the step")
	}
	got, err := fileSHA256(path)
	if err != nil {
		return fmt.Errorf("executable: %w", err)
	}
	if got != want {
		return fmt.Errorf("executable %s changed since install (sha256 %s, installed %s); reinstall the step", filepath.Base(path), got, want)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path) // #nosec G304 -- installed step executable under the steps directory
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// stepResult is the step's stdout (ADR-058 StepResult).
type stepResult struct {
	Object json.RawMessage `json:"object"`
	Error  *StepError      `json:"error"`
}

// StepError is the error a step reports.
type StepError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
	// Retryable asks the job system to retry the object; by default a
	// reported error fails it for good.
	Retryable bool `json:"retryable,omitempty"`
}

func (e *StepError) asError(step string) error {
	code := e.Code
	if code == "" {
		code = "STEP_FAILED"
	}
	err := fmt.Errorf("%s: step reported %s: %s", step, code, e.Message)
	if e.Retryable {
		return err
	}
	return pipeline.Permanent(err)
}

func parseResult(stdout []byte) (stepResult, error) {
	var r stepResult
	if len(bytes.TrimSpace(stdout)) == 0 {
		return r, errors.New("invalid output: empty stdout")
	}
	dec := json.NewDecoder(bytes.NewReader(stdout))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return r, fmt.Errorf("invalid output: not a StepResult JSON object: %w", err)
	}
	if dec.More() {
		return r, errors.New("invalid output: data after the StepResult object")
	}
	if bytes.Equal(bytes.TrimSpace(r.Object), []byte("null")) {
		r.Object = nil
	}
	return r, nil
}

// Fields a step may rewrite, by JSON name. Everything else on the object
// is identity, provenance or lifecycle, which only dpkms sets: a step's
// output may repeat those values but never change them.
var writableFields = map[string]bool{
	"type":         true,
	"subtype":      true,
	"raw_content":  true,
	"text_content": true,
	"content_type": true,
	"metadata":     true,
	"summaries":    true,
	"sections":     true,
	"tags":         true,
	"mentions":     true,
	"decisions":    true,
	"tasks":        true,
	"graph":        true,
	"inbox_note":   true,
}

// WritableFields lists the object fields a step may change, sorted.
func WritableFields() []string {
	out := make([]string, 0, len(writableFields))
	for k := range writableFields {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// koFields maps each serialized KnowledgeObject field's JSON name to its
// struct field index.
var koFields = func() map[string]int {
	m := map[string]int{}
	t := reflect.TypeOf(storage.KnowledgeObject{})
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		m[name] = i
	}
	return m
}()

// MergeStepOutput applies a step's output object to in. Only the fields
// the output carries count: an omitted field keeps its value. A writable
// field is replaced; any other field must equal in's value; an unknown
// field is an error. Fields that never serialize (vectors, blob
// content) carry over from in.
func MergeStepOutput(in *storage.KnowledgeObject, raw json.RawMessage) (*storage.KnowledgeObject, error) {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil || keys == nil {
		return nil, fmt.Errorf("invalid output: object is not a JSON object")
	}
	var parsed storage.KnowledgeObject
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("invalid output: object: %w", err)
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)

	out := *in
	inV := reflect.ValueOf(in).Elem()
	outV := reflect.ValueOf(&out).Elem()
	parsedV := reflect.ValueOf(&parsed).Elem()
	for _, k := range names {
		idx, ok := koFields[k]
		if !ok {
			return nil, fmt.Errorf("invalid output: unknown object field %q", k)
		}
		if writableFields[k] {
			outV.Field(idx).Set(parsedV.Field(idx))
			continue
		}
		was, _ := json.Marshal(inV.Field(idx).Interface())
		now, _ := json.Marshal(parsedV.Field(idx).Interface())
		if !bytes.Equal(was, now) {
			// Values stay out of the message: it lands in the job's
			// error, and the object's content must not.
			return nil, fmt.Errorf("invalid output: field %q is read-only for steps and was changed; steps may change only %s",
				k, strings.Join(WritableFields(), ", "))
		}
	}
	return &out, nil
}
