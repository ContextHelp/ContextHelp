// Package medium implements MediumStrategy parent + Publication and
// Profile children. Captures may arrive on medium.com, on a
// publication-owned subdomain (e.g. blog.medium.com), or on a
// custom-domain Medium publication; the customdomain detector
// classifies the latter and the strategies use that result so the
// catch-all handles all three.
package medium

import (
	"context"
	"net/url"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

const (
	IDParent      = "MediumStrategy"
	IDPublication = "MediumPublicationStrategy"
	IDProfile     = "MediumProfileStrategy"
)

const (
	CandidateTypeArticle     = "medium_article"
	CandidateTypeAuthor      = "medium_author"
	CandidateTypePublication = "medium_publication"
	CandidateTypeFeed        = "medium_feed"
	CandidateTypeTag         = "medium_tag"
)

// MediumClient is the daemon-side fetcher. The strategy uses it
// optionally to resolve a custom-domain capture into the canonical
// medium.com surface (publication slug + author username).
type MediumClient interface {
	ResolvePublication(ctx context.Context, host string) (slug string, err error)
	ResolveAuthor(ctx context.Context, articleURL string) (username string, err error)
}

// hintsFromEvent lifts capture-pipeline hints off CapturedEvent.Hints
// for the customdomain detector. The substrate threads
// MetaPlatform / Generator / CanonicalHost through ev.Hints;
// strategies just forward the typed value here so the detector input
// is uniform across packages.
func hintsFromEvent(ev lateral.CapturedEvent) lateral.Hints { return ev.Hints }

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

func articleSlugFromPath(parts []string) (author, slug string) {
	// Forms:
	//  /@username/article-title-abc123
	//  /publication-slug/article-title-abc123
	//  /p/abc123 (legacy)
	if len(parts) >= 2 && strings.HasPrefix(parts[0], "@") {
		return strings.TrimPrefix(parts[0], "@"), parts[1]
	}
	if len(parts) >= 2 && parts[0] == "p" {
		return "", parts[1]
	}
	if len(parts) >= 2 {
		return "", parts[0] + "/" + parts[1]
	}
	return "", ""
}
