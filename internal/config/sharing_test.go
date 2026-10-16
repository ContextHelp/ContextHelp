package config

import (
	"bytes"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The sharing section decodes from its documented keys, strictly.
func TestSharingConfig_YAML(t *testing.T) {
	doc := `
classes:
  no_defaults: true
  rules:
    - class: personal
      structured_type: person_note
    - class: financial
      tag: invoices
      pipeline: text.long
`
	dec := yaml.NewDecoder(bytes.NewReader([]byte(doc)))
	dec.KnownFields(true)
	var c SharingConfig
	if err := dec.Decode(&c); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !c.Classes.NoDefaults || len(c.Classes.Rules) != 2 {
		t.Fatalf("decoded %+v", c)
	}
	if r := c.Classes.Rules[1]; r.Class != "financial" || r.Tag != "invoices" || r.Pipeline != "text.long" {
		t.Errorf("rule[1] = %+v", r)
	}
	if errs := c.Validate(); len(errs) != 0 {
		t.Errorf("Validate = %v", errs)
	}

	dec = yaml.NewDecoder(bytes.NewReader([]byte("classes:\n  rules:\n    - class: x\n      bogus: y\n")))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("unknown key: %v", err)
	}
}

func TestSharingConfig_Validate(t *testing.T) {
	for _, tc := range []struct {
		name      string
		rules     []ClassRuleConfig
		wantField string
	}{
		{"no rules", nil, ""},
		{"ok", []ClassRuleConfig{{Class: "personal", Profile: "personal"}}, ""},
		{"bad class", []ClassRuleConfig{{Class: "Not Valid", Type: "note"}}, "sharing.classes.rules[0].class"},
		{"no matcher", []ClassRuleConfig{{Class: "personal"}}, "sharing.classes.rules[0]"},
		{"second rule bad", []ClassRuleConfig{{Class: "personal", Type: "note"}, {Class: "", Type: "note"}}, "sharing.classes.rules[1].class"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := SharingConfig{Classes: ClassesConfig{Rules: tc.rules}}.Validate()
			if tc.wantField == "" {
				if len(errs) != 0 {
					t.Fatalf("Validate = %v, want none", errs)
				}
				return
			}
			if len(errs) == 0 || errs[0].Field != tc.wantField {
				t.Fatalf("Validate = %v, want field %s", errs, tc.wantField)
			}
		})
	}
	// Validate reports it through the config validator.
	c := &Config{Sharing: SharingConfig{Classes: ClassesConfig{Rules: []ClassRuleConfig{{Class: "personal"}}}}}
	found := false
	for _, e := range Validate(c) {
		if strings.HasPrefix(e.Field, "sharing.classes.rules") {
			found = true
		}
	}
	if !found {
		t.Error("Validate(config) did not report the sharing rule")
	}
}
