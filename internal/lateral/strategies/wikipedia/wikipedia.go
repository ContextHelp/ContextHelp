// Package wikipedia implements the WikipediaStrategy. It handles all
// language editions of *.wikipedia.org and emits sub-path probes that
// expose the article's lateral surfaces — its talk page, revision
// history, the categories that frame it, the cross-language interwiki
// links, and (when a WikipediaClient is wired) the canonical Wikidata
// item.
package wikipedia

import (
	"context"
	"net/url"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
)

const ID = "WikipediaStrategy"

const (
	CandidateTypeArticle    = "wiki_article"
	CandidateTypeTalk       = "wiki_talk"
	CandidateTypeHistory    = "wiki_history"
	CandidateTypeCategories = "wiki_categories"
	CandidateTypeInterwiki  = "wiki_interwiki"
	CandidateTypeWikidata   = "wiki_wikidata"
)

// WikipediaClient is the daemon-side fetcher. ResolveWikidata returns
// the Q-id for a given (lang, title) pair, when available.
type WikipediaClient interface {
	ResolveWikidata(ctx context.Context, lang, title string) (qID string, err error)
}

type Strategy struct {
	Client WikipediaClient
}

func New(c WikipediaClient) *Strategy { return &Strategy{Client: c} }

func (*Strategy) ID() string                    { return ID }
func (*Strategy) Family() lateral.StrategyFamily { return lateral.FamilyPlatform }
func (*Strategy) Preconditions() []string       { return nil }

// Applies matches any *.wikipedia.org host. Specificity = 2 (domain +
// language subdomain), or 1 for the apex (which redirects to en.).
func (*Strategy) Applies(_ context.Context, ev lateral.CapturedEvent) lateral.AppliesResult {
	host := hostOf(ev.SourceURL)
	if host == "wikipedia.org" {
		return lateral.AppliesResult{Matches: true, Specificity: 1}
	}
	if strings.HasSuffix(host, ".wikipedia.org") {
		return lateral.AppliesResult{Matches: true, Specificity: 2}
	}
	return lateral.AppliesResult{}
}

func (s *Strategy) Probe(ctx context.Context, ev lateral.CapturedEvent, _ lateral.ActiveContext) ([]lateral.Candidate, error) {
	u, err := url.Parse(ev.SourceURL)
	if err != nil {
		return nil, err
	}
	host := strings.ToLower(u.Hostname())
	lang := "en"
	if host != "wikipedia.org" && strings.HasSuffix(host, ".wikipedia.org") {
		lang = strings.TrimSuffix(host, ".wikipedia.org")
		// Skip non-language subdomains (m., en.m., commons., species., etc.).
		if i := strings.LastIndex(lang, "."); i >= 0 {
			lang = lang[i+1:]
		}
	}

	parts := pathSegments(u.Path)
	if len(parts) < 2 || parts[0] != "wiki" {
		return nil, nil
	}
	title := strings.Join(parts[1:], "/")
	if title == "" {
		return nil, nil
	}
	// Skip pseudo-namespaces that aren't articles.
	for _, ns := range []string{"Special:", "File:", "Help:", "Wikipedia:", "Portal:", "Category:", "Talk:", "User:", "User_talk:"} {
		if strings.HasPrefix(title, ns) {
			return nil, nil
		}
	}

	apex := "https://" + lang + ".wikipedia.org"
	idKey := identitykey.BuildLocalised("wikipedia", identitykey.EntityArticle, lang, title)

	out := []lateral.Candidate{
		{
			URL:           apex + "/wiki/" + title,
			CandidateType: CandidateTypeArticle,
			Strategy:      ID,
			Preview:       identitykey.Set(map[string]any{"lang": lang, "title": title}, idKey),
		},
		{
			URL:           apex + "/wiki/Talk:" + title,
			CandidateType: CandidateTypeTalk,
			Strategy:      ID,
			Preview:       identitykey.Set(map[string]any{"lang": lang, "title": title, "facet": "talk"}, idKey),
		},
		{
			URL:           apex + "/w/index.php?title=" + title + "&action=history",
			CandidateType: CandidateTypeHistory,
			Strategy:      ID,
			Preview:       identitykey.Set(map[string]any{"lang": lang, "title": title, "facet": "history"}, idKey),
		},
		{
			URL:           apex + "/wiki/Special:WhatLinksHere/" + title,
			CandidateType: CandidateTypeCategories,
			Strategy:      ID,
			Preview:       identitykey.Set(map[string]any{"lang": lang, "title": title, "facet": "backlinks"}, idKey),
		},
	}

	// Cross-language interwiki anchor (action=langlinks API endpoint).
	out = append(out, lateral.Candidate{
		URL:           apex + "/w/api.php?action=query&prop=langlinks&titles=" + title + "&format=json",
		CandidateType: CandidateTypeInterwiki,
		Strategy:      ID,
		Preview:       identitykey.Set(map[string]any{"lang": lang, "title": title, "facet": "interwiki"}, idKey),
	})

	if s.Client != nil {
		if qid, err := s.Client.ResolveWikidata(ctx, lang, title); err == nil && qid != "" {
			out = append(out, lateral.Candidate{
				URL:           "https://www.wikidata.org/wiki/" + qid,
				CandidateType: CandidateTypeWikidata,
				Strategy:      ID,
				Preview:       identitykey.Set(map[string]any{"qid": qid}, identitykey.Build("wikidata", "item", qid)),
			})
		}
	}

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
