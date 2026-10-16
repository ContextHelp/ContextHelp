package sharing

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ClassRule assigns Class to the objects every set matcher holds for
// (ADR-080 §3, decision D5). An unset matcher matches anything; a rule
// with no matcher is invalid.
type ClassRule struct {
	Class string
	// Type, Subtype, Source, Profile match the object's columns.
	Type, Subtype, Source, Profile string
	// Pipeline matches the object's pipeline with or without its @vN
	// suffix.
	Pipeline string
	// Tag matches any of the object's tag labels.
	Tag string
	// StructuredType matches the type the structured_metadata step
	// assigned (person_note, idea, ...).
	StructuredType string
}

// Structured types the built-in rules know.
const (
	// StructuredPersonNote is the structured_metadata type of a note
	// about a person.
	StructuredPersonNote = "person_note"
	// ProfilePersonal is the profile the built-in rule classes personal.
	ProfilePersonal = "personal"
	// metadataStructured is the metadata key the structured_metadata
	// step writes its result under.
	metadataStructured = "enrichment.structured_metadata"
)

// DefaultClassRules are the built-in rules: a typed person note and the
// personal profile are personal.
func DefaultClassRules() []ClassRule {
	return []ClassRule{
		{Class: storage.ClassPersonal, StructuredType: StructuredPersonNote},
		{Class: storage.ClassPersonal, Profile: ProfilePersonal},
	}
}

// Validate checks the rule names a valid class and at least one matcher.
func (r ClassRule) Validate() error {
	if err := storage.ValidateClassName(r.Class); err != nil {
		return err
	}
	if r.Type == "" && r.Subtype == "" && r.Source == "" && r.Profile == "" && r.Pipeline == "" && r.Tag == "" && r.StructuredType == "" {
		return errors.New("class rule: set at least one matcher")
	}
	return nil
}

// Matches reports whether every set matcher holds for obj.
func (r ClassRule) Matches(obj *storage.KnowledgeObject) bool {
	if obj == nil {
		return false
	}
	if r.Type != "" && obj.Type != r.Type {
		return false
	}
	if r.Subtype != "" && obj.Subtype != r.Subtype {
		return false
	}
	if r.Source != "" && obj.Source != r.Source {
		return false
	}
	if r.Profile != "" && obj.ProfileID != r.Profile {
		return false
	}
	if r.Pipeline != "" && !pipelineMatches(obj.Pipeline, r.Pipeline) {
		return false
	}
	if r.Tag != "" && !hasTag(obj, r.Tag) {
		return false
	}
	if r.StructuredType != "" && StructuredType(obj) != r.StructuredType {
		return false
	}
	return true
}

// pipelineMatches compares pipeline names ignoring an @vN suffix on
// either side.
func pipelineMatches(have, want string) bool {
	h, _, _ := strings.Cut(have, "@")
	w, _, _ := strings.Cut(want, "@")
	return have == want || h == w
}

func hasTag(obj *storage.KnowledgeObject, label string) bool {
	for _, t := range obj.Tags {
		if t.Label == label {
			return true
		}
	}
	return false
}

// StructuredType returns the type the structured_metadata step assigned
// to obj, whether the draft still holds the step's value or the stored
// object its JSON form; "" when none.
func StructuredType(obj *storage.KnowledgeObject) string {
	if obj == nil {
		return ""
	}
	v, ok := obj.Metadata[metadataStructured]
	if !ok || v == nil {
		return ""
	}
	if m, ok := v.(map[string]any); ok {
		s, _ := m["type"].(string)
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	var typed struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(b, &typed); err != nil {
		return ""
	}
	return typed.Type
}

// ApplyClassRules returns obj's classes with the class of every matching
// rule added: sorted, unique, nil when empty. obj is not modified.
func ApplyClassRules(rules []ClassRule, obj *storage.KnowledgeObject) []string {
	if obj == nil {
		return nil
	}
	classes := append([]string(nil), obj.Classes...)
	for _, r := range rules {
		if r.Matches(obj) {
			classes = append(classes, r.Class)
		}
	}
	return storage.NormalizeClasses(classes)
}

// ValidateClassRules checks every rule, naming the first bad one.
func ValidateClassRules(rules []ClassRule) error {
	for i, r := range rules {
		if err := r.Validate(); err != nil {
			return fmt.Errorf("class rule %d: %w", i, err)
		}
	}
	return nil
}
