// Package customdomain provides a shared detector that maps a captured
// URL onto a known publishing platform (Medium, Substack, Beehiiv) when
// the URL is hosted on a publication's own custom domain rather than the
// platform's canonical host.
//
// The detector intentionally returns only a coarse platform tag plus the
// effective host. The per-platform strategy interprets the URL further
// (publication, post, etc.) once the platform is known.
//
// Detection signals (best-effort, in declared order):
//
//  1. Canonical host suffix (e.g. *.substack.com, *.medium.com,
//     *.beehiiv.com). Cheapest, no fetch.
//  2. Caller-supplied page hints — meta tags, generator strings, link
//     rels — that the captured-page record already carries. Strategies
//     pass these in via Hints when available.
//
// DNS-based resolution (CNAME → known platform) is intentionally out of
// scope for v1: it requires network reachability the strategy layer
// doesn't have, and is best handled at capture time by the daemon.
package customdomain

import (
	"net/url"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/lateral"
)

// Platform identifies the publishing platform a URL belongs to.
type Platform string

const (
	PlatformUnknown  Platform = ""
	PlatformMedium   Platform = "medium"
	PlatformSubstack Platform = "substack"
	PlatformBeehiiv  Platform = "beehiiv"
)

// Hints aliases lateral.Hints so callers that only import customdomain
// keep compiling, but the canonical home for the type is the substrate
// (lateral package) — the Hints field lives on lateral.CapturedEvent.
//
// Deprecated: use lateral.Hints directly. This alias stays for one
// minor cycle to ease migration.
type Hints = lateral.Hints

// Detection is the detector's verdict for one URL.
type Detection struct {
	Platform   Platform // PlatformUnknown if no signal matched
	Host       string   // effective host (lowercased, port stripped)
	CustomHost bool     // true when matched via custom-domain hints, not canonical suffix
}

// Detect classifies rawURL. hints is optional; pass the zero value
// (lateral.Hints{}) when no page-side signals are available, in which
// case only canonical-host detection runs.
func Detect(rawURL string, hints lateral.Hints) Detection {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return Detection{}
	}
	host := strings.ToLower(u.Hostname())
	d := Detection{Host: host}

	// Layer 1: canonical host suffix.
	switch {
	case host == "medium.com" || strings.HasSuffix(host, ".medium.com"):
		d.Platform = PlatformMedium
		return d
	case strings.HasSuffix(host, ".substack.com") || host == "substack.com":
		d.Platform = PlatformSubstack
		return d
	case strings.HasSuffix(host, ".beehiiv.com") || host == "beehiiv.com":
		d.Platform = PlatformBeehiiv
		return d
	}

	// Layer 2: explicit platform hint takes precedence over generator/canonical
	// — the capture pipeline has more context than meta-tag scraping.
	if hints.MetaPlatform != "" {
		switch strings.ToLower(hints.MetaPlatform) {
		case "medium":
			d.Platform, d.CustomHost = PlatformMedium, true
			return d
		case "substack":
			d.Platform, d.CustomHost = PlatformSubstack, true
			return d
		case "beehiiv":
			d.Platform, d.CustomHost = PlatformBeehiiv, true
			return d
		}
	}

	// Layer 3: generator string. Casing varies, so lowercase compare.
	gen := strings.ToLower(hints.Generator)
	switch {
	case strings.Contains(gen, "substack"):
		d.Platform, d.CustomHost = PlatformSubstack, true
		return d
	case strings.Contains(gen, "beehiiv"):
		d.Platform, d.CustomHost = PlatformBeehiiv, true
		return d
	case strings.Contains(gen, "medium"):
		d.Platform, d.CustomHost = PlatformMedium, true
		return d
	}

	// Layer 4: canonical host suffix on rel=canonical.
	canon := strings.ToLower(hints.CanonicalHost)
	switch {
	case strings.HasSuffix(canon, ".substack.com"):
		d.Platform, d.CustomHost = PlatformSubstack, true
		return d
	case strings.HasSuffix(canon, ".beehiiv.com"):
		d.Platform, d.CustomHost = PlatformBeehiiv, true
		return d
	case canon == "medium.com" || strings.HasSuffix(canon, ".medium.com"):
		d.Platform, d.CustomHost = PlatformMedium, true
		return d
	}

	return d
}

// IsHostedOn reports whether host is a canonical host of platform (any
// subdomain or the apex). Convenience for strategies that want a quick
// boolean without parsing a full URL.
func IsHostedOn(host string, platform Platform) bool {
	host = strings.ToLower(host)
	switch platform {
	case PlatformMedium:
		return host == "medium.com" || strings.HasSuffix(host, ".medium.com")
	case PlatformSubstack:
		return host == "substack.com" || strings.HasSuffix(host, ".substack.com")
	case PlatformBeehiiv:
		return host == "beehiiv.com" || strings.HasSuffix(host, ".beehiiv.com")
	}
	return false
}
