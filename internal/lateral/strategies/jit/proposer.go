package jit

import (
	"context"
	"strings"
)

// Proposer is the lateral-side abstraction for an LLM that proposes sub-paths
// to crawl for a given source domain and page type. The concrete implementation
// is host-wired (production: a kit/ai/llm.Completer adapter; tests: a fake)
// — this package only declares the contract, the prompt builder, and the
// response parser. Decoupled from any specific LLM client so the jit package
// stays domain-focused (mirrors the pattern in pageshape/llm.go).
//
// Returned slice elements may be relative paths ("/about", "/team/$slug") or
// absolute URLs; the executor (T-0246) is responsible for resolution against
// the source domain.
type Proposer interface {
	Propose(ctx context.Context, domain string, pageType string) ([]string, error)
}

// ProposalRequest is the structured input passed to BuildPrompt. The host
// extracts the page-type string from a pageshape.Label before calling.
type ProposalRequest struct {
	SourceDomain string
	PageType     string
}

// proposalPromptTemplate is the wire prompt sent to the host LLM. Kept short
// and structured: one sub-path per line, no prose, capped at ~10. The parser
// (ParseProposalResponse) defends against deviation (bullets, comments,
// duplicates, runaway length).
const proposalPromptTemplate = `You are proposing lateral-discovery sub-paths for a web capture system.

Source domain: %s
Page type: %s

List up to 10 sub-paths on this domain that are likely to surface related entities (people, projects, posts) worth capturing. Output rules:

- One sub-path per line.
- Relative paths starting with "/" (e.g. /about, /team) or absolute URLs on the same domain.
- No bullets, no numbering, no prose, no commentary.
- No blank lines.

Sub-paths:
`

// BuildPrompt returns the literal prompt text the host sends to the LLM.
// Deterministic: identical input yields byte-equal output.
func BuildPrompt(req ProposalRequest) string {
	// Avoid fmt to keep zero-allocation determinism trivial; explicit
	// substitution makes the template easy to audit.
	out := proposalPromptTemplate
	out = strings.Replace(out, "%s", req.SourceDomain, 1)
	out = strings.Replace(out, "%s", req.PageType, 1)
	return out
}

// maxParsedProposals caps the parser output. Defensive: the prompt asks for
// 10, but a runaway LLM shouldn't be able to fill memory with thousands of
// lines. 25 leaves some slack above the prompt's stated 10.
const maxParsedProposals = 25

// ParseProposalResponse cleans a raw LLM response into a slice of sub-paths.
// Steps: split on newlines (CRLF tolerant), trim whitespace, strip leading
// "- " or "* " bullets, drop empty + comment lines ("#" or "//"), dedupe
// preserving first occurrence, cap at maxParsedProposals.
func ParseProposalResponse(raw string) []string {
	if raw == "" {
		return nil
	}
	// Normalize CRLF → LF so a single split handles both.
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")

	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))

	for _, ln := range lines {
		s := strings.TrimSpace(ln)
		if s == "" {
			continue
		}
		// Strip a single leading bullet marker.
		switch {
		case strings.HasPrefix(s, "- "):
			s = strings.TrimSpace(s[2:])
		case strings.HasPrefix(s, "* "):
			s = strings.TrimSpace(s[2:])
		}
		if s == "" {
			continue
		}
		// Drop comments. Note: this matches "#" / "//" at start only;
		// inline comments are preserved (sub-paths may legitimately contain
		// "#" fragments).
		if strings.HasPrefix(s, "#") || strings.HasPrefix(s, "//") {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
		if len(out) >= maxParsedProposals {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
