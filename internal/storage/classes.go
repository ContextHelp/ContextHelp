package storage

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Content classes (ADR-080 §3): sensitivity labels on an object
// (objects.classes) or on a part of one (a graph node's metadata.classes).
// A share policy excludes by class and a principal's ceiling denies by
// class, whatever the data filter says.

// Classes the built-in rules and the structured types assign.
const (
	// ClassPersonal marks the owner's private notes: typed person notes,
	// the personal profile, a contact's vCard NOTE.
	ClassPersonal = "personal"
	// ClassContactDetails marks a contact's email, phone and address.
	ClassContactDetails = "contact_details"
)

// NodeMetadataClasses is the graph node metadata key that carries a
// part's classes, a JSON array of class names.
const NodeMetadataClasses = "classes"

// classNamePattern bounds a class name: a lowercase word, as policies
// and config name them.
var classNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// ValidateClassName reports whether name may label an object or a part.
func ValidateClassName(name string) error {
	if !classNamePattern.MatchString(name) {
		return fmt.Errorf("class %q: want 1-64 chars of [a-z0-9_-], starting with a letter", name)
	}
	return nil
}

// NormalizeClasses returns classes sorted, without duplicates or empty
// entries; nil when nothing is left.
func NormalizeClasses(classes []string) []string {
	out := make([]string, 0, len(classes))
	for _, c := range classes {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	slices.Sort(out)
	out = slices.Compact(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

// ClassesJSON encodes classes as the JSON array the classes columns
// hold, normalized; nil encodes as [].
func ClassesJSON(classes []string) (string, error) {
	norm := NormalizeClasses(classes)
	if norm == nil {
		norm = []string{}
	}
	b, err := json.Marshal(norm)
	if err != nil {
		return "", fmt.Errorf("encode classes: %w", err)
	}
	return string(b), nil
}

// ParseClassesJSON decodes a classes column; an empty column and [] both
// decode to nil.
func ParseClassesJSON(s string) ([]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var classes []string
	if err := json.Unmarshal([]byte(s), &classes); err != nil {
		return nil, fmt.Errorf("decode classes %q: %w", s, err)
	}
	return NormalizeClasses(classes), nil
}

// NodeClasses returns the classes a graph node carries under
// NodeMetadataClasses, as written by a step ([]string) or as read back
// from JSON ([]any).
func NodeClasses(n GraphNode) []string {
	switch v := n.Metadata[NodeMetadataClasses].(type) {
	case []string:
		return NormalizeClasses(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return NormalizeClasses(out)
	}
	return nil
}

// SetNodeClasses writes classes on n under NodeMetadataClasses; nil
// removes the key.
func SetNodeClasses(n *GraphNode, classes []string) {
	norm := NormalizeClasses(classes)
	if norm == nil {
		delete(n.Metadata, NodeMetadataClasses)
		return
	}
	if n.Metadata == nil {
		n.Metadata = map[string]any{}
	}
	n.Metadata[NodeMetadataClasses] = norm
}
