// Package llm wires hop.top/kit/go/ai/llm.Completer into the lateral
// jit strategy's Proposer + PageTypeClassifier interfaces.
//
// jit declares two LLM-backed contracts:
//
//   - jit.Proposer: produces lateral-discovery sub-paths for a given
//     (domain, page-type) tuple. Built around jit.BuildPrompt +
//     jit.ParseProposalResponse so the on-wire prompt and the parser
//     stay colocated with the strategy that owns the contract.
//   - jit.PageTypeClassifier: maps a source URL to a single page-type
//     string. The pageshape package owns the multi-label classifier;
//     T-0317 wraps that for jit. This package's PageTypeClassifier is
//     a fallback when the daemon doesn't wire pageshape (small CLI
//     runs, smoke tests).
//
// Decoupling: the jit package never imports kit/ai/llm. This adapter is
// the only surface where the kit Completer crosses into lateral.
package llm

import (
	"context"
	"fmt"
	"strings"

	kitllm "hop.top/kit/go/ai/llm"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/jit"
)

// Completer is the narrow subset of kit/ai/llm.Completer this adapter
// depends on. Defining it here lets tests inject a stub without
// pulling kit's full client surface, and lets the daemon swap in a
// fallback chain wrapping multiple Completers via kit/ai/llm.Client.
//
// kitllm.Completer satisfies this interface verbatim (Complete is the
// only method); the alias is purely for testability.
type Completer interface {
	Complete(ctx context.Context, req kitllm.Request) (kitllm.Response, error)
}

// Options tunes the adapter at construction time. Zero-value Options
// is fine: Model defaults to "gpt-4o-mini" (the cheapest commonly-
// available chat model in 2026) and Temperature to 0.2 (low for
// determinism, non-zero so stuck-prompt mode opens).
type Options struct {
	// Model is the LLM model identifier passed through to the
	// Completer. Empty falls back to "gpt-4o-mini".
	Model string

	// Temperature is the sampling temperature. Zero falls back to
	// 0.2; explicit override possible by setting any non-zero value.
	Temperature float64

	// MaxTokens caps the response length. Zero falls back to 256
	// (sub-path lists rarely need more than ~100 tokens).
	MaxTokens int
}

// defaultModel mirrors the kit/ai/llm.Request.Model contract: when
// empty, the adapter passes a sane default rather than letting kit's
// router reject the call. Operators override via Options.Model.
const defaultModel = "gpt-4o-mini"

// defaultTemperature is low enough that ParseProposalResponse's
// dedup/parse stays useful (LLMs at temp=1.0 wander into prose),
// non-zero so the model exits "stuck prompt" mode where it might
// repeat the same first sub-path indefinitely.
const defaultTemperature = 0.2

// defaultMaxTokens caps the proposal response. The prompt asks for
// up to 10 sub-paths; 256 tokens leaves room for slightly verbose
// LLMs without permitting runaway output.
const defaultMaxTokens = 256

// Proposer wraps a kit Completer to satisfy jit.Proposer. Each
// Propose call builds the prompt via jit.BuildPrompt, sends a
// single-message Request, and parses the response with
// jit.ParseProposalResponse.
type Proposer struct {
	llm  Completer
	opts Options
}

// NewProposer wires the adapter. llm is the kit Completer (or any
// type that implements the Completer subset); opts tunes per-call
// behaviour.
//
// NewProposer panics if llm is nil — fail loud at construction
// rather than nil-pointer in Propose.
func NewProposer(llm Completer, opts Options) *Proposer {
	if llm == nil {
		panic("llm.NewProposer: llm is nil")
	}
	if opts.Model == "" {
		opts.Model = defaultModel
	}
	if opts.Temperature == 0 {
		opts.Temperature = defaultTemperature
	}
	if opts.MaxTokens == 0 {
		opts.MaxTokens = defaultMaxTokens
	}
	return &Proposer{llm: llm, opts: opts}
}

// Propose builds the JIT prompt, sends it through the wrapped
// Completer, and parses the response. Errors from the Completer
// propagate; an empty response produces (nil, nil) rather than an
// error — JIT treats "no proposals" as a valid outcome.
//
// Compile-time guarantee: jit.Proposer.Propose signature.
var _ jit.Proposer = (*Proposer)(nil)

func (p *Proposer) Propose(ctx context.Context, domain string, pageType string) ([]string, error) {
	prompt := jit.BuildPrompt(jit.ProposalRequest{
		SourceDomain: domain,
		PageType:     pageType,
	})
	// kit Request.Temperature is a pointer so an explicit zero stays
	// distinguishable from unset; opts.Temperature is already
	// normalised to a non-zero default in New.
	temp := p.opts.Temperature
	resp, err := p.llm.Complete(ctx, kitllm.Request{
		Model: p.opts.Model,
		Messages: []kitllm.Message{
			{Role: "user", Content: prompt},
		},
		Temperature: &temp,
		MaxTokens:   p.opts.MaxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("llm.Propose: %w", err)
	}
	return jit.ParseProposalResponse(resp.Content), nil
}

// PageTypeClassifier wraps a kit Completer to satisfy
// jit.PageTypeClassifier without depending on pageshape. The classifier
// asks the LLM "what kind of page is this?" using a minimal prompt and
// returns the trimmed first line.
//
// This is the fallback adapter — production deployments wire the
// pageshape adapter (T-0317) which composes a heuristic + recipe-cache
// stack. Use this when you want jit to have *some* page-type signal
// in environments where the heuristic stack isn't available.
type PageTypeClassifier struct {
	llm  Completer
	opts Options
}

// NewPageTypeClassifier wires the LLM-only classifier. Same panic-on-
// nil-llm contract as NewProposer.
func NewPageTypeClassifier(llm Completer, opts Options) *PageTypeClassifier {
	if llm == nil {
		panic("llm.NewPageTypeClassifier: llm is nil")
	}
	if opts.Model == "" {
		opts.Model = defaultModel
	}
	if opts.Temperature == 0 {
		opts.Temperature = defaultTemperature
	}
	if opts.MaxTokens == 0 {
		// page-type labels are short — 64 tokens is plenty.
		opts.MaxTokens = 64
	}
	return &PageTypeClassifier{llm: llm, opts: opts}
}

// classifyPromptTemplate is a one-shot prompt asking for a single
// page-type label. Kept short and structured so the response is
// trivially parseable: first non-empty line wins.
const classifyPromptTemplate = `Classify the page at this URL into ONE label from: BlogPost, Blog, AboutPage, ProductPage, PricingPage, DocsPage, LandingPage, Business, ResearchPaper, JobPosting, EventPage, EcommerceStore, Other.

URL: %s

Respond with ONLY the label, no prose.

Label:`

// Classify returns the trimmed first non-empty line of the LLM's
// response. Empty / whitespace-only responses produce ("", nil) —
// jit's noPageType safely handles the empty case (proposer still
// gets the source domain).
//
// Compile-time guarantee: jit.PageTypeClassifier.Classify signature.
var _ jit.PageTypeClassifier = (*PageTypeClassifier)(nil)

func (c *PageTypeClassifier) Classify(ctx context.Context, sourceURL string) (string, error) {
	prompt := strings.Replace(classifyPromptTemplate, "%s", sourceURL, 1)
	// See Propose: kit takes *float64 so an explicit zero is not
	// mistaken for unset.
	temp := c.opts.Temperature
	resp, err := c.llm.Complete(ctx, kitllm.Request{
		Model: c.opts.Model,
		Messages: []kitllm.Message{
			{Role: "user", Content: prompt},
		},
		Temperature: &temp,
		MaxTokens:   c.opts.MaxTokens,
	})
	if err != nil {
		return "", fmt.Errorf("llm.Classify: %w", err)
	}
	for _, ln := range strings.Split(resp.Content, "\n") {
		s := strings.TrimSpace(ln)
		if s != "" {
			return s, nil
		}
	}
	return "", nil
}
