package mentions

import (
	"strings"

	"hop.top/uri"
)

// Parse converts a mention string to a ctxt:// entity URI.
// It accepts two forms:
//   - @namespace.slug  (e.g. "@stripe.api.checkout")
//   - ctxt://entity/… (already a URI, passed through)
//
// All dot separators in the slug are converted to path segments, so
// @stripe.api.checkout becomes ctxt://entity/stripe/api/checkout.
func Parse(s string) (uri.URI, bool) {
	if strings.HasPrefix(s, "ctxt://") {
		u, err := uri.Parse(s)
		if err != nil {
			return uri.URI{}, false
		}
		return *u, true
	}
	s = strings.TrimPrefix(s, "@")
	s = strings.ReplaceAll(s, ".", "/")
	if s == "" {
		return uri.URI{}, false
	}
	parts := strings.SplitN(s, "/", 2)
	if len(parts) == 2 {
		return uri.URI{Scheme: "ctxt", Space: "entity", ID: parts[0] + "/" + parts[1]}, true
	}
	return uri.URI{Scheme: "ctxt", Space: "entity", ID: s}, true
}

// ParseSlice converts a slice of mention strings (either format) to URIs,
// silently dropping any that are empty or unparseable.
func ParseSlice(ss []string) []uri.URI {
	uris := make([]uri.URI, 0, len(ss))
	for _, s := range ss {
		if u, ok := Parse(s); ok {
			uris = append(uris, u)
		}
	}
	return uris
}
