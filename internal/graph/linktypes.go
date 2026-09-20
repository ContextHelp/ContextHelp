// Package graph — link type vocabulary for associative object links (US-0406).
package graph

import "fmt"

// LinkType is a typed relationship between two knowledge objects.
type LinkType string

const (
	LinkExtends        LinkType = "extends"
	LinkExtendedBy     LinkType = "extended-by"
	LinkContradicts    LinkType = "contradicts"
	LinkContradictedBy LinkType = "contradicted-by"
	LinkSupersedes     LinkType = "supersedes"
	LinkSupersededBy   LinkType = "superseded-by"
	LinkSupports       LinkType = "supports"
	LinkSupportedBy    LinkType = "supported-by"
	LinkRelatedTo      LinkType = "related-to"
	LinkDerivedFrom    LinkType = "derived-from"
	LinkDerivedTo      LinkType = "derived-to"
)

// inverseMap maps each link type to its inverse.
var inverseMap = map[LinkType]LinkType{
	LinkExtends:        LinkExtendedBy,
	LinkExtendedBy:     LinkExtends,
	LinkContradicts:    LinkContradictedBy,
	LinkContradictedBy: LinkContradicts,
	LinkSupersedes:     LinkSupersededBy,
	LinkSupersededBy:   LinkSupersedes,
	LinkSupports:       LinkSupportedBy,
	LinkSupportedBy:    LinkSupports,
	LinkRelatedTo:      LinkRelatedTo, // symmetric
	LinkDerivedFrom:    LinkDerivedTo,
	LinkDerivedTo:      LinkDerivedFrom,
}

// AllLinkTypes returns the set of valid link types for user-facing commands.
// Excludes inverse-only types (extended-by, contradicted-by, etc.) that are
// created automatically.
var UserLinkTypes = []LinkType{
	LinkExtends,
	LinkContradicts,
	LinkSupersedes,
	LinkSupports,
	LinkRelatedTo,
	LinkDerivedFrom,
}

// ValidLinkType returns true if lt is a known link type (including inverses).
func ValidLinkType(lt LinkType) bool {
	_, ok := inverseMap[lt]
	return ok
}

// InverseLinkType returns the inverse of lt. Returns error for unknown types.
func InverseLinkType(lt LinkType) (LinkType, error) {
	inv, ok := inverseMap[lt]
	if !ok {
		return "", fmt.Errorf("unknown link type %q", lt)
	}
	return inv, nil
}

// IsSymmetric returns true if the link type equals its own inverse.
func IsSymmetric(lt LinkType) bool {
	inv, ok := inverseMap[lt]
	return ok && inv == lt
}

// UserLinkTypeStrings returns string slice of user-facing link types.
func UserLinkTypeStrings() []string {
	out := make([]string, len(UserLinkTypes))
	for i, lt := range UserLinkTypes {
		out[i] = string(lt)
	}
	return out
}
