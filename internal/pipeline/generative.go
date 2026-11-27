package pipeline

import (
	"context"
	"net/url"
	"slices"
	"strings"

	"golang.org/x/net/http/httpproxy"
	kitllm "hop.top/kit/go/ai/llm"
)

// ProviderUse is one provider a step calls, as the step was built.
type ProviderUse struct {
	// Resource is ResourceLLM, ResourceVision or ResourceEmbedding.
	Resource string
	// Backend is the Name of the provider the step holds, "" for none.
	Backend string
	// Endpoint is the URL a local backend calls (the resolved default when
	// none is configured); "" for a hosted backend or when unknown.
	Endpoint string
	// Usable is false for no provider, or a provider that stands in for a
	// missing one (a backend that cannot run, or the stub).
	Usable bool
	// Stub is set when the provider is the stub.
	Stub bool
}

// ProviderStep is implemented by every step that declares the llm, vision
// or embedding resource. It reports what the step was built with, which
// the contract cannot: a step built without a provider degrades to
// heuristics instead of failing.
type ProviderStep interface {
	ProviderUses(ctx context.Context) []ProviderUse
}

// OpaqueStep marks a step that runs outside the process (an installed
// step): it may call any provider and reports none.
type OpaqueStep interface {
	UsesUnknownProviders()
}

// unwrapped yields step and every step it wraps (a contract override).
func unwrapped(step PipelineStep) []PipelineStep {
	var out []PipelineStep
	for step != nil {
		out = append(out, step)
		w, ok := step.(interface{ Unwrap() PipelineStep })
		if !ok {
			break
		}
		step = w.Unwrap()
	}
	return out
}

// AsProviderStep is step as a ProviderStep, looking through wrappers that
// expose the step they wrap with Unwrap (a contract override).
func AsProviderStep(step PipelineStep) (ProviderStep, bool) {
	for _, s := range unwrapped(step) {
		if g, ok := s.(ProviderStep); ok {
			return g, true
		}
	}
	return nil, false
}

func isOpaque(step PipelineStep) bool {
	for _, s := range unwrapped(step) {
		if _, ok := s.(OpaqueStep); ok {
			return true
		}
	}
	return false
}

// GenerativeResourceOf is the generative resource a contract declares, or
// "" when it declares none.
func GenerativeResourceOf(c StepContract) string {
	for _, r := range c.Resources {
		if r == ResourceLLM || r == ResourceVision {
			return r
		}
	}
	return ""
}

// PrunedStep is a step left out of a pipeline because a capability it
// needs was unmet when the pipeline was built.
type PrunedStep struct {
	Name      string
	Resources []string // as the step's contract declares them
}

// Generative reports the generative resource the pruned step declares.
func (p PrunedStep) Generative() string {
	return GenerativeResourceOf(StepContract{Resources: p.Resources})
}

// Locality says where a pipeline's provider calls go. It fails closed:
// anything it cannot positively judge is not Automatic.
type Locality string

const (
	// LocalityNone: no step calls a provider.
	LocalityNone Locality = "none"
	// LocalityLocal: every provider call stays on this machine or its
	// local network.
	LocalityLocal Locality = "local"
	// LocalityHosted: at least one provider call goes to a hosted service.
	LocalityHosted Locality = "hosted"
	// LocalityUnknown: the pipeline holds a step that is not a built-in or
	// that may call a model of its own, and reports no provider use, so
	// where its data goes cannot be told.
	LocalityUnknown Locality = "unknown"
	// LocalityUnavailable: a generative step was pruned, holds no usable
	// provider, or holds the stub, or the pipeline is not built at all:
	// it cannot do its work.
	LocalityUnavailable Locality = "unavailable"
)

// Automatic reports whether work through the pipeline may run without
// asking: nothing leaves the local network, and nothing is missing.
func (l Locality) Automatic() bool { return l == LocalityNone || l == LocalityLocal }

// StepUse is one step's provider use, or the reason it cannot be judged.
type StepUse struct {
	Step string
	ProviderUse
	// Pruned: the step was left out of the pipeline for a missing
	// capability.
	Pruned bool
	// Opaque: the step may call any provider and reports none.
	Opaque bool
}

// LocalityReport is the classification of a pipeline, with the steps that
// decided it (or that a caller may want to report).
type LocalityReport struct {
	Locality Locality
	// Uses lists each provider use, each pruned generative step, each
	// opaque step, and each network step (resource "network", which does
	// not change the Locality: it fetches the object's own source).
	Uses []StepUse
	// Reason says why a pipeline that is not built is unavailable.
	Reason string
}

// backendIsLocal reports whether a backend name runs on a machine the
// operator controls: ollama (and "auto", which only resolves to local
// Ollama). It says nothing about where that backend is: see useLocality.
// Any other name is hosted.
func backendIsLocal(backend string) bool {
	switch strings.ToLower(backend) {
	case "ollama", "ollama-embed", "auto":
		return true
	}
	return false
}

// useLocality classifies one usable provider use. A local backend is local
// when its endpoint is loopback or on the private network, as
// llm.EndpointLocality judges by the host alone: hostnames (Docker and LAN
// names included) are always hosted, because no DNS lookup is made. A
// private endpoint that the environment's proxy settings would send
// through a proxy is hosted. A hosted backend, a remote or unparsable
// endpoint, and an empty one (nothing to judge) are hosted.
func useLocality(u ProviderUse) Locality {
	if !backendIsLocal(u.Backend) || u.Endpoint == "" {
		return LocalityHosted
	}
	loc, err := kitllm.EndpointLocality(u.Endpoint)
	switch {
	case err != nil || (loc != kitllm.LocalityLoopback && loc != kitllm.LocalityPrivate):
		return LocalityHosted
	case loc == kitllm.LocalityPrivate && viaProxy(u.Endpoint):
		return LocalityHosted
	}
	return LocalityLocal
}

// viaProxy reports whether a request to rawURL would go through a proxy
// under the standard environment settings (loopback never does).
func viaProxy(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	p, err := httpproxy.FromEnvironment().ProxyFunc()(u)
	return err != nil || p != nil
}

var localityRank = map[Locality]int{LocalityNone: 0, LocalityLocal: 1, LocalityHosted: 2, LocalityUnknown: 3, LocalityUnavailable: 4}

// ClassifyPipeline classifies p by the steps it holds, worst first:
// unavailable, unknown, hosted, local, none. known says whether a step is
// a built-in: any other step is unknown, marker or not.
//
// The embedding step is judged as it runs: a model whose provider cannot
// be built is skipped by the step and listed here as unusable, without
// making the pipeline unavailable; with no usable model the step embeds
// nothing and adds no locality.
func ClassifyPipeline(ctx context.Context, p *Pipeline, known func(PipelineStep) bool) LocalityReport {
	rep := LocalityReport{Locality: LocalityNone}
	note := func(l Locality) {
		if localityRank[l] > localityRank[rep.Locality] {
			rep.Locality = l
		}
	}
	for _, pr := range p.Pruned {
		if res := pr.Generative(); res != "" {
			rep.Uses = append(rep.Uses, StepUse{Step: pr.Name, ProviderUse: ProviderUse{Resource: res}, Pruned: true})
			note(LocalityUnavailable)
		}
	}
	for _, st := range p.Steps {
		classifyStep(ctx, st, known, &rep, note)
	}
	return rep
}

func classifyStep(ctx context.Context, st PipelineStep, known func(PipelineStep) bool, rep *LocalityReport, note func(Locality)) {
	opaque := func() {
		rep.Uses = append(rep.Uses, StepUse{Step: st.Name(), Opaque: true})
		note(LocalityUnknown)
	}
	if isOpaque(st) {
		opaque()
		return
	}
	// A step that is not a built-in is unknown whatever it reports: it may
	// wrap a built-in and run something else.
	if known == nil || !known(st) {
		opaque()
		return
	}
	if ps, ok := AsProviderStep(st); ok {
		for _, u := range ps.ProviderUses(ctx) {
			shown := u
			shown.Endpoint = kitllm.RedactURI(u.Endpoint)
			rep.Uses = append(rep.Uses, StepUse{Step: st.Name(), ProviderUse: shown})
			switch {
			case u.Usable:
				note(useLocality(u))
			case u.Resource != ResourceEmbedding:
				note(LocalityUnavailable)
			}
		}
		return
	}
	if res := GenerativeResourceOf(st.Contract()); res != "" {
		// declares a provider and cannot say what it holds
		rep.Uses = append(rep.Uses, StepUse{Step: st.Name(), ProviderUse: ProviderUse{Resource: res}})
		note(LocalityUnavailable)
		return
	}
	if slices.Contains(st.Contract().Resources, ResourceNetwork) {
		rep.Uses = append(rep.Uses, StepUse{Step: st.Name(), ProviderUse: ProviderUse{Resource: ResourceNetwork, Usable: true}})
	}
}

// ProviderLocality is ClassifyPipeline for the registered pipeline name
// (bare or versioned), as built. A pipeline left out of the registry for
// a missing provider is LocalityUnavailable with its reason; a name that
// is not registered at all is an error.
func ProviderLocality(ctx context.Context, r Registry, name string, known func(PipelineStep) bool) (LocalityReport, error) {
	p, err := r.Get(name)
	if err != nil {
		if rep, ok := UnavailableReport(r, name); ok {
			return rep, nil
		}
		return LocalityReport{}, err
	}
	return ClassifyPipeline(ctx, p, known), nil
}

// UnavailableReport is the report of a pipeline name the registry knows
// only as unavailable (left out for a missing provider).
func UnavailableReport(r Registry, name string) (LocalityReport, bool) {
	bare := name
	if parsed, _, ok := ParseVersionedName(name); ok {
		bare = parsed
	}
	if reason, ok := UnavailableReason(r, bare); ok {
		return LocalityReport{Locality: LocalityUnavailable, Reason: reason}, true
	}
	return LocalityReport{}, false
}
