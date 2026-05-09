// Package arxiv implements the ArxivStrategy for arxiv.org.
//
// Captured input is typically an /abs/<id> page. The strategy proposes:
//
//   - The PDF URL                     /pdf/<id>
//   - Each listed author              /a/<author> (when client resolves)
//   - The Semantic Scholar mirror     (when SemanticScholarLookup set)
//
// Author resolution requires a daemon-side ArxivClient because arxiv.org's
// /abs page lists authors as plain HTML, not in URL form. When no client
// is wired the strategy still emits the abs/pdf/version pair so dedup
// works on identity_key.
package arxiv

import (
	"context"
	"net/url"
	"regexp"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

const ID = "ArxivStrategy"

const (
	CandidateTypePaper   = "arxiv_paper"
	CandidateTypePDF     = "arxiv_pdf"
	CandidateTypeAuthor  = "arxiv_author"
	CandidateTypeMirror  = "arxiv_mirror"
	CandidateTypeVersion = "arxiv_version"
)

// ArxivClient is the daemon-side fetcher. ListAuthors returns the author
// slugs (or display names) for a given paper ID.
type ArxivClient interface {
	ListAuthors(ctx context.Context, paperID string) (authors []string, err error)
}

type Strategy struct {
	Client ArxivClient
}

func New(c ArxivClient) *Strategy { return &Strategy{Client: c} }

func (*Strategy) ID() string                    { return ID }
func (*Strategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }
func (*Strategy) Preconditions() []string       { return nil }

func (*Strategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	host := hostOf(ev.SourceURL)
	if host == "arxiv.org" || strings.HasSuffix(host, ".arxiv.org") {
		spec := 1
		if host != "arxiv.org" {
			spec++
		}
		return lateral.AppliesResult{Matches: true, Specificity: spec}
	}
	return lateral.AppliesResult{}
}

// arXiv ID pattern (post-2007): NNNN.NNNNN with optional vN suffix.
// Pre-2007 IDs (cs.LG/0303001) supported via the slash form.
var paperIDRegexp = regexp.MustCompile(`^([a-zA-Z][a-zA-Z\-\.]*/[0-9]+|[0-9]{4}\.[0-9]{4,5})(v[0-9]+)?$`)

func (s *Strategy) Probe(ctx context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	u, err := url.Parse(ev.SourceURL)
	if err != nil {
		return nil, err
	}
	parts := pathSegments(u.Path)
	if len(parts) < 2 {
		return nil, nil
	}
	surface, rest := parts[0], parts[1:]

	// Acceptable surfaces: abs, pdf, html, format, ps.
	switch surface {
	case "abs", "pdf", "html", "format", "ps":
	default:
		return nil, nil
	}

	// Stitch the remaining segments back together — pre-2007 IDs use a
	// slash (cs.LG/0303001) which path.Split splits on.
	id := strings.Join(rest, "/")
	id = strings.TrimSuffix(id, ".pdf")
	if !paperIDRegexp.MatchString(id) {
		return nil, nil
	}
	// Strip version suffix for the canonical id; keep the version tag
	// for the version probe.
	canonID := id
	version := ""
	if i := strings.LastIndex(id, "v"); i > 0 && id[i:] != "" {
		// Only treat as version if everything after 'v' is digits.
		ver := id[i+1:]
		if isAllDigits(ver) {
			canonID = id[:i]
			version = id[i:]
		}
	}

	apex := "https://arxiv.org"
	out := []lateral.Candidate{
		{
			URL:           apex + "/abs/" + canonID,
			CandidateType: CandidateTypePaper,
			Strategy:      ID,
			Preview:       identitykey.Set(map[string]any{"id": canonID}, identitykey.Build("arxiv", identitykey.EntityPaper, canonID)),
		},
		{
			URL:           apex + "/pdf/" + canonID,
			CandidateType: CandidateTypePDF,
			Strategy:      ID,
			Preview:       identitykey.Set(map[string]any{"id": canonID, "format": "pdf"}, identitykey.Build("arxiv", identitykey.EntityPaper, canonID)),
		},
	}

	if version != "" {
		out = append(out, lateral.Candidate{
			URL:           apex + "/abs/" + canonID + version,
			CandidateType: CandidateTypeVersion,
			Strategy:      ID,
			Preview:       identitykey.Set(map[string]any{"id": canonID, "version": version}, identitykey.Build("arxiv", identitykey.EntityPaper, canonID+version)),
		})
	}

	if s.Client != nil {
		authors, err := s.Client.ListAuthors(ctx, canonID)
		if err == nil {
			for _, a := range authors {
				slug := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(a), " ", "_"))
				if slug == "" {
					continue
				}
				out = append(out, lateral.Candidate{
					URL:           apex + "/a/" + slug + ".html",
					CandidateType: CandidateTypeAuthor,
					Strategy:      ID,
					Preview:       identitykey.Set(map[string]any{"name": a}, identitykey.Build("arxiv", identitykey.EntityProfile, slug)),
				})
			}
		}
	}

	// Always emit Semantic Scholar mirror probe — substrate's resolver
	// uses the corresponding identity_key to dedup if a different
	// strategy independently surfaces the same paper.
	out = append(out, lateral.Candidate{
		URL:           "https://www.semanticscholar.org/arxiv?query=" + canonID,
		CandidateType: CandidateTypeMirror,
		Strategy:      ID,
		Preview:       identitykey.Set(map[string]any{"id": canonID, "mirror": "semanticscholar"}, identitykey.Build("arxiv", identitykey.EntityPaper, canonID)),
	})

	return out, nil
}

func hostOf(s string) string {
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func pathSegments(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
