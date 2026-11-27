package steps

import (
	"context"
	"reflect"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/providers/vision"
)

// llmUses is the pipeline.ProviderStep report of a step holding p. The
// stub is detected by type, as the capability check does.
func llmUses(p providers.LLMProvider) []pipeline.ProviderUse {
	u := pipeline.ProviderUse{Resource: pipeline.ResourceLLM}
	if p == nil {
		return []pipeline.ProviderUse{u}
	}
	_, u.Stub = p.(*providers.StubLLMProvider)
	u.Backend, u.Endpoint = p.Name(), providers.EndpointOf(p)
	u.Usable = !u.Stub && !providers.IsFailed(p)
	return []pipeline.ProviderUse{u}
}

// visionUses is llmUses for a vision provider.
func visionUses(p vision.Provider) []pipeline.ProviderUse {
	u := pipeline.ProviderUse{Resource: pipeline.ResourceVision}
	if p == nil {
		return []pipeline.ProviderUse{u}
	}
	_, u.Stub = p.(*vision.StubProvider)
	u.Backend, u.Endpoint = p.Name(), providers.EndpointOf(p)
	u.Usable = !u.Stub && !providers.IsFailed(p)
	return []pipeline.ProviderUse{u}
}

// ProviderUses of the embedding step: the provider of each model it would
// populate, as the resolver builds it now. A model whose provider cannot
// be built is unusable; with no model source or resolver the step embeds
// nothing and uses no provider.
func (s *EmbeddingGenerator) ProviderUses(ctx context.Context) []pipeline.ProviderUse {
	if s.models == nil || s.resolver == nil {
		return nil
	}
	unusable := pipeline.ProviderUse{Resource: pipeline.ResourceEmbedding}
	models, err := s.models.Populating(ctx, time.Now())
	if err != nil {
		return []pipeline.ProviderUse{unusable}
	}
	var out []pipeline.ProviderUse
	for _, m := range models {
		p, err := s.resolver.ForModel(ctx, m)
		if err != nil || p == nil {
			out = append(out, unusable)
			continue
		}
		u := pipeline.ProviderUse{Resource: pipeline.ResourceEmbedding, Backend: p.Name(), Endpoint: providers.EndpointOf(p)}
		u.Stub = u.Backend == "stub" // the stub embedding provider is unexported
		u.Usable = !u.Stub
		out = append(out, u)
	}
	return out
}

// IsBuiltin reports whether step is one of the built-in steps: a value of a
// type defined in this package, looking through contract overrides. An
// installed step is not, whatever it is called.
func IsBuiltin(step pipeline.PipelineStep) bool {
	for step != nil {
		t := reflect.TypeOf(step)
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.PkgPath() == builtinPkg {
			return true
		}
		w, ok := step.(interface{ Unwrap() pipeline.PipelineStep })
		if !ok {
			return false
		}
		step = w.Unwrap()
	}
	return false
}

// builtinPkg is this package's import path.
var builtinPkg = reflect.TypeOf(EmbeddingGenerator{}).PkgPath()
