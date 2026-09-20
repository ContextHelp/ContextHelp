package steps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type StepExecutor struct {
	store        storage.StorageDriver
	builtinSteps map[string]pipeline.PipelineStep
	stepsPath    string
	checkDocker  func() bool
}

func NewStepExecutor(store storage.StorageDriver, stepsPath string) *StepExecutor {
	return &StepExecutor{
		store:        store,
		builtinSteps: make(map[string]pipeline.PipelineStep),
		stepsPath:    stepsPath,
		checkDocker: func() bool {
			_, err := exec.LookPath("docker")
			return err == nil
		},
	}
}

func (se *StepExecutor) RegisterBuiltin(name string, step pipeline.PipelineStep) {
	se.builtinSteps[name] = step
}

func (se *StepExecutor) LoadRegisteredSteps(ctx context.Context) error {
	steps, _, err := se.store.Steps().List(ctx, "")
	if err != nil {
		return fmt.Errorf("list steps: %w", err)
	}

	for _, step := range steps {
		if step.Source == "builtin" {
			continue
		}

		execStep := &ExternalStep{
			name: step.Name,
			path: step.Path,
		}

		se.RegisterBuiltin(step.Name, execStep)
	}

	return nil
}

func (se *StepExecutor) ExecuteStep(ctx context.Context, name string, config map[string]any, sandbox *storage.SandboxConfig, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	step, ok := se.builtinSteps[name]
	if !ok {
		return nil, fmt.Errorf("step %s not found", name)
	}

	if sandbox != nil && sandbox.Enabled {
		if sandbox.IsolationLevel == "container" {
			if !se.checkDocker() {
				return nil, fmt.Errorf("container isolation requires Docker")
			}
			return se.executeWithContainer(ctx, name, config, sandbox, draft)
		}
		return se.executeWithProcess(ctx, name, config, sandbox, draft)
	}

	return step.Run(ctx, draft)
}

func (se *StepExecutor) executeWithProcess(ctx context.Context, name string, config map[string]any, sandbox *storage.SandboxConfig, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	step, err := se.store.Steps().Get(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("get step: %w", err)
	}

	execPath := se.getStepExecutablePath(step)
	if execPath == "" {
		return nil, fmt.Errorf("no executable found for step %s", name)
	}

	input := map[string]any{
		"config": config,
		"object": draft,
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}

	cmd := exec.CommandContext(ctx, execPath) // #nosec G204 -- execPath is a validated step binary from registry
	cmd.Stdin = strings.NewReader(string(inputJSON))

	if sandbox != nil {
		if sandbox.ResourceLimits != nil {
			if sandbox.ResourceLimits.MaxMemory != "" {
				if err := se.setMemoryLimit(sandbox.ResourceLimits.MaxMemory); err != nil {
					return nil, fmt.Errorf("set memory limit: %w", err)
				}
			}
			if sandbox.ResourceLimits.MaxCPU != "" {
				if err := se.setCPULimit(sandbox.ResourceLimits.MaxCPU); err != nil {
					return nil, fmt.Errorf("set CPU limit: %w", err)
				}
			}
		}

		cmd.Env = append(os.Environ(), "SANDBOX_ENABLED=1")
		if sandbox.Filesystem != nil {
			if sandbox.Filesystem.ReadOnly {
				cmd.Env = append(cmd.Env, "SANDBOX_READONLY=1")
			}
			if !sandbox.Filesystem.WriteAllowed {
				cmd.Env = append(cmd.Env, "SANDBOX_NOWRITE=1")
			}
		}
		if !sandbox.Network {
			cmd.Env = append(cmd.Env, "SANDBOX_NONETWORK=1")
		}
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("execute step: %w, output: %s", err, string(output))
	}

	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("unmarshal output: %w", err)
	}

	if errMsg, ok := result["error"].(string); ok {
		return nil, fmt.Errorf("step error: %s", errMsg)
	}

	objRaw, ok := result["object"]
	if !ok {
		return nil, fmt.Errorf("invalid output format: missing object field")
	}

	objBytes, err := json.Marshal(objRaw)
	if err != nil {
		return nil, fmt.Errorf("re-marshal object: %w", err)
	}

	var obj storage.KnowledgeObject
	if err := json.Unmarshal(objBytes, &obj); err != nil {
		return nil, fmt.Errorf("unmarshal object: %w", err)
	}

	return &obj, nil
}

func (se *StepExecutor) executeWithContainer(ctx context.Context, name string, config map[string]any, sandbox *storage.SandboxConfig, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	step, err := se.store.Steps().Get(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("get step: %w", err)
	}

	input := map[string]any{
		"config": config,
		"object": draft,
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}

	image := "contexthelp/step:latest"
	if step.Metadata != nil && step.Metadata.Version != "" {
		image = fmt.Sprintf("contexthelp/step-%s:%s", step.Name, step.Metadata.Version)
	}

	args := []string{"run", "--rm"}

	if sandbox != nil && sandbox.ResourceLimits != nil {
		if sandbox.ResourceLimits.MaxMemory != "" {
			args = append(args, "--memory="+sandbox.ResourceLimits.MaxMemory)
		}
		if sandbox.ResourceLimits.MaxCPU != "" {
			args = append(args, "--cpus="+sandbox.ResourceLimits.MaxCPU)
		}
		if sandbox.ResourceLimits.Timeout != "" {
			timeout, err := time.ParseDuration(sandbox.ResourceLimits.Timeout)
			if err == nil {
				args = append(args, "--timeout="+timeout.String())
			}
		}
	}

	args = append(args,
		"-v", se.stepsPath+":/steps:ro",
		"-e", "INPUT_JSON="+string(inputJSON),
		image,
	)

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = os.Environ()

	if sandbox != nil && !sandbox.Network {
		args = append([]string{"run", "--rm", "--network=none"}, args[2:]...)
		cmd = exec.CommandContext(ctx, "docker", args...)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("execute container: %w, output: %s", err, string(output))
	}

	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("unmarshal output: %w", err)
	}

	if errMsg, ok := result["error"].(string); ok {
		return nil, fmt.Errorf("step error: %s", errMsg)
	}

	objRaw, ok := result["object"]
	if !ok {
		return nil, fmt.Errorf("invalid output format: missing object field")
	}

	objBytes, err := json.Marshal(objRaw)
	if err != nil {
		return nil, fmt.Errorf("re-marshal object: %w", err)
	}

	var obj storage.KnowledgeObject
	if err := json.Unmarshal(objBytes, &obj); err != nil {
		return nil, fmt.Errorf("unmarshal object: %w", err)
	}

	return &obj, nil
}

func (se *StepExecutor) getStepExecutablePath(step *storage.RegisteredStep) string {
	if step.Path == "" {
		return ""
	}

	execPath := filepath.Join(step.Path, "bin", "step")
	if _, err := os.Stat(execPath); err == nil {
		return execPath
	}

	execPath = filepath.Join(step.Path, "step")
	if _, err := os.Stat(execPath); err == nil {
		return execPath
	}

	return ""
}

// setMemoryLimit and setCPULimit are implemented in executor_unix.go (darwin/linux)
// and executor_windows.go (windows) via build tags.

func parseMemoryLimit(limit string) uint64 {
	var value float64
	var unit string

	n, _ := fmt.Sscanf(limit, "%f%s", &value, &unit)
	if n < 1 {
		return 512 * 1024 * 1024
	}

	switch strings.ToUpper(unit) {
	case "MB":
		return uint64(value * 1024 * 1024)
	case "GB":
		return uint64(value * 1024 * 1024 * 1024)
	default:
		return uint64(value)
	}
}

func parseCPULimit(limit string) uint64 {
	var value float64
	fmt.Sscanf(limit, "%f", &value)
	return uint64(value)
}

type ExternalStep struct {
	pipeline.BaseContract
	name string
	path string
}

func (es *ExternalStep) Name() string { return es.name }

func (es *ExternalStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	return draft, nil
}
