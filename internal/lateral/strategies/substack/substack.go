// Package substack implements SubstackStrategy parent + Publication,
// Post, and Notes children. The customdomain detector handles the
// custom-domain → substack mapping; strategies here treat both
// *.substack.com hosts and custom-domain hosts uniformly.
package substack

import (
	"context"
	"net/url"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/customdomain"
)

const (
	IDParent      = "SubstackStrategy"
	IDPublication = "SubstackPublicationStrategy"
	IDPost        = "SubstackPostStrategy"
	IDNotes       = "SubstackNotesStrategy"
)

const (
	CandidateTypePost        = "substack_post"
	CandidateTypePublication = "substack_publication"
	CandidateTypeAuthor      = "substack_author"
	CandidateTypeNote        = "substack_note"
	CandidateTypeFeed        = "substack_feed"
	CandidateTypeArchive     = "substack_archive"
)

// SubstackClient is the daemon-side fetcher. ResolvePublication maps a
// custom-domain host to its canonical *.substack.com slug when known.
type SubstackClient interface {
	ResolvePublication(ctx context.Context, host string) (slug string, err error)
}

func parseURL(rawURL string) (*url.URL, []string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, err
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 1 && parts[0] == "" {
		parts = nil
	}
	return u, parts, nil
}

func hintsFromEvent(_ lateral.CapturedEvent) customdomain.Hints { return customdomain.Hints{} }

// canonicalSlug resolves the publication's slug from host when it is a
// substack subdomain; falls back to host for custom domains. Client is
// optional; nil disables custom-domain resolution.
func canonicalSlug(ctx context.Context, host string, client SubstackClient) string {
	host = strings.ToLower(host)
	if strings.HasSuffix(host, ".substack.com") {
		return strings.TrimSuffix(host, ".substack.com")
	}
	if client != nil {
		if slug, err := client.ResolvePublication(ctx, host); err == nil && slug != "" {
			return slug
		}
	}
	return host
}

// canonicalRoot returns the *.substack.com URL root for slug.
func canonicalRoot(slug string) string {
	if strings.Contains(slug, ".") {
		// Custom domain unresolvable — keep the host form.
		return "https://" + slug
	}
	return "https://" + slug + ".substack.com"
}
