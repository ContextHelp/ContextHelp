// Package beehiiv implements BeehiivStrategy parent + Publication and
// Post children. The customdomain detector handles canonical (
// *.beehiiv.com) and custom-domain Beehiiv publications uniformly.
package beehiiv

import (
	"context"
	"net/url"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

const (
	IDParent      = "BeehiivStrategy"
	IDPublication = "BeehiivPublicationStrategy"
	IDPost        = "BeehiivPostStrategy"
)

const (
	CandidateTypePublication = "beehiiv_publication"
	CandidateTypePost        = "beehiiv_post"
	CandidateTypeFeed        = "beehiiv_feed"
	CandidateTypeAuthor      = "beehiiv_author"
	CandidateTypeArchive     = "beehiiv_archive"
)

// BeehiivClient is the daemon-side fetcher. ResolvePublication maps a
// custom-domain host to its canonical *.beehiiv.com slug.
type BeehiivClient interface {
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

// hintsFromEvent lifts capture-pipeline hints off CapturedEvent.Hints
// for the customdomain detector. Returns the zero value when
// the substrate hasn't populated Hints, preserving pre-substrate
// canonical-host-only detection.
func hintsFromEvent(ev lateral.CapturedEvent) lateral.Hints { return ev.Hints }

// canonicalSlug derives the publication slug from the host. *.beehiiv.com
// gives the slug as the leftmost label; custom domains use the host
// itself unless the daemon-side client resolves a real slug.
func canonicalSlug(ctx context.Context, host string, c BeehiivClient) string {
	host = strings.ToLower(host)
	if strings.HasSuffix(host, ".beehiiv.com") {
		return strings.TrimSuffix(host, ".beehiiv.com")
	}
	if c != nil {
		if slug, err := c.ResolvePublication(ctx, host); err == nil && slug != "" {
			return slug
		}
	}
	return host
}

// canonicalRoot returns the URL root for a slug. Resolved slugs map to
// *.beehiiv.com; unresolved custom-domain hosts stay on the host.
func canonicalRoot(slug string) string {
	if strings.Contains(slug, ".") {
		return "https://" + slug
	}
	return "https://" + slug + ".beehiiv.com"
}
