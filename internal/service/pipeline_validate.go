package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"hop.top/kit/go/runtime/domain"

	"github.com/ideacrafterslabs/ctxt/internal/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ErrProtectedPipeline is returned by CreatePipeline for a name a built-in
// pipeline owns: an existing built-in, or the namespace of one (text.*).
var ErrProtectedPipeline = errors.New("protected pipeline name")

// ErrStepValidationUnavailable is returned by CreatePipeline when the
// pipeline names steps but the service has no built-in step predicate
// (WithReservedStepNames), so it cannot tell a built-in from a typo.
// Validation fails closed rather than accept unknown steps.
var ErrStepValidationUnavailable = errors.New("step validation unavailable: no built-in step predicate configured")

// builtinPipelineNamespace is the namespace the built-in text pipelines
// own whether or not they are registered in this process.
const builtinPipelineNamespace = "text"

// protectedPipelineName reports whether name is a built-in pipeline or in
// the namespace (the segment before the first dot) of one.
func (s *Service) protectedPipelineName(name string) bool {
	if s.Pipes == nil {
		return false
	}
	if _, err := s.Pipes.Get(name); err == nil {
		return true
	}
	ns, _, dotted := strings.Cut(name, ".")
	if !dotted {
		return false
	}
	if ns == builtinPipelineNamespace {
		return true
	}
	for _, registered := range s.Pipes.List() {
		if rns, _, ok := strings.Cut(registered, "."); ok && rns == ns {
			return true
		}
	}
	return false
}

// validatePipelineCreate checks a new pipeline before it is persisted:
// protected names, step names (built-in or installed) and the sandbox.
// Duplicate names are the repository's unique constraint, surfaced as
// domain.ErrConflict.
func (s *Service) validatePipelineCreate(ctx context.Context, p *storage.Pipeline) error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("%w: pipeline name is required", domain.ErrValidation)
	}
	if s.protectedPipelineName(p.Name) {
		return fmt.Errorf("%w: pipeline names starting with %q or matching a built-in pipeline are protected: %q",
			ErrProtectedPipeline, builtinPipelineNamespace+".", p.Name)
	}
	// Reserved is the built-in step predicate (WithReservedStepNames);
	// without it a built-in cannot be told from a typo, so refuse.
	if len(p.Steps) > 0 && s.Installer.Reserved == nil {
		return ErrStepValidationUnavailable
	}
	unknown := make([]string, 0, len(p.Steps))
	for _, st := range p.Steps {
		if s.Installer.Reserved(st.Name) {
			continue
		}
		if rs, err := s.Store(ctx).Steps().Get(ctx, st.Name); err == nil && rs != nil {
			continue
		}
		unknown = append(unknown, st.Name)
	}
	if len(unknown) > 0 {
		return fmt.Errorf("%w: step %s is not registered (dpkms pipeline step list)",
			domain.ErrValidation, strings.Join(unknown, ", "))
	}
	return validateSandbox(p.Sandbox)
}

// validateSandbox checks a pipeline's sandbox: a supported isolation level,
// parseable resource limits, and no network access.
func validateSandbox(sb *storage.SandboxConfig) error {
	if sb == nil {
		return nil
	}
	switch sb.IsolationLevel {
	case "", "process", "container":
	default:
		return fmt.Errorf("%w: sandbox.isolation_level %q: want process or container", domain.ErrValidation, sb.IsolationLevel)
	}
	if sb.Network {
		return fmt.Errorf("%w: sandbox.network: network access is not allowed in a sandboxed pipeline", domain.ErrValidation)
	}
	if rl := sb.ResourceLimits; rl != nil {
		if err := steps.ValidateResources(storage.StepResources{MaxMemory: rl.MaxMemory, MaxCPU: rl.MaxCPU, Timeout: rl.Timeout}); err != nil {
			return fmt.Errorf("%w: sandbox.resource_limits: %w", domain.ErrValidation, err)
		}
	}
	return nil
}
