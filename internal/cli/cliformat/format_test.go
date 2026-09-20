package cliformat

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// withFormat pins the active format for one test and restores it after.
// These tests never run a cobra command, so Active falls through to
// viper and no Bind is needed.
func withFormat(t *testing.T, value string) {
	t.Helper()
	prev := viper.GetString("format")
	viper.Set("format", value)
	t.Cleanup(func() { viper.Set("format", prev) })
}

func TestValidateRejectsUnknownFormat(t *testing.T) {
	err := Validate("bogus-format")
	if err == nil {
		t.Fatal("an unknown format must be rejected, not accepted")
	}
	// The message must echo the rejected value AND name the valid set:
	// a caller that guessed wrong has to be able to correct itself
	// without reading the docs.
	msg := err.Error()
	if !strings.Contains(msg, "bogus-format") {
		t.Errorf("error should echo the rejected value, got: %s", msg)
	}
	for _, valid := range Valid() {
		if !strings.Contains(msg, valid) {
			t.Errorf("error should name valid format %q, got: %s", valid, msg)
		}
	}
}

func TestValidateAcceptsEveryAdvertisedFormat(t *testing.T) {
	// Whatever Valid() advertises must actually be accepted; otherwise
	// the error message sends callers toward a value that then fails.
	for _, format := range Valid() {
		if err := Validate(format); err != nil {
			t.Errorf("Validate(%q) should accept an advertised format: %v", format, err)
		}
	}
}

func TestValidateAcceptsUnsetFormat(t *testing.T) {
	if err := Validate(""); err != nil {
		t.Errorf("an unset format must be accepted, got: %v", err)
	}
}

func TestStructuredCoversJSONAndYAML(t *testing.T) {
	for format, want := range map[string]bool{
		"json":     true,
		"yaml":     true,
		"table":    false,
		"markdown": false,
		"md":       false,
		"csv":      false,
		"text":     false,
		"human":    false,
	} {
		if got := IsStructured(format); got != want {
			t.Errorf("IsStructured(%q) = %v, want %v", format, got, want)
		}
	}
}

// TestEncodeEmptyCollectionsAreNotNull pins the interoperability fix:
// a caller doing `for x in result.jobs` must get an empty list, not a
// null it has to special-case.
func TestEncodeEmptyCollectionsAreNotNull(t *testing.T) {
	withFormat(t, "json")

	t.Run("bare nil slice", func(t *testing.T) {
		var rows []string
		var buf bytes.Buffer
		if err := Encode(&buf, rows); err != nil {
			t.Fatalf("encode: %v", err)
		}
		got := strings.TrimSpace(buf.String())
		if got != "[]" {
			t.Errorf("a nil slice must encode as [], got %s", got)
		}
	})

	t.Run("nil slice inside an envelope", func(t *testing.T) {
		var jobs []string
		var buf bytes.Buffer
		if err := Encode(&buf, map[string]any{"jobs": jobs, "total": 0}); err != nil {
			t.Fatalf("encode: %v", err)
		}
		var got map[string]any
		if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
			t.Fatalf("output should be valid JSON: %v", err)
		}
		list, ok := got["jobs"].([]any)
		if !ok {
			t.Fatalf("jobs must decode as a list, got %T (%v)", got["jobs"], got["jobs"])
		}
		if len(list) != 0 {
			t.Errorf("jobs should be empty, got %v", list)
		}
	})

	t.Run("populated slice is untouched", func(t *testing.T) {
		var buf bytes.Buffer
		if err := Encode(&buf, []string{"a", "b"}); err != nil {
			t.Fatalf("encode: %v", err)
		}
		var got []string
		if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
			t.Fatalf("output should be valid JSON: %v", err)
		}
		if len(got) != 2 || got[0] != "a" || got[1] != "b" {
			t.Errorf("normalization must not alter a populated slice, got %v", got)
		}
	})
}

func TestEncodeHonoursYAML(t *testing.T) {
	withFormat(t, "yaml")

	var buf bytes.Buffer
	if err := Encode(&buf, map[string]any{"total": 7}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out := buf.String()
	// YAML, not JSON: the key must be bare rather than quoted-and-braced.
	if strings.Contains(out, "{") {
		t.Errorf("yaml output should not be JSON-shaped, got: %s", out)
	}
	if !strings.Contains(out, "total: 7") {
		t.Errorf("yaml output should carry the mapping, got: %s", out)
	}
}

// TestEncodeYAMLUsesJSONFieldNames pins the property that makes
// --format yaml a real alternative to --format json rather than a
// second, undocumented vocabulary: both encodings name the same field
// the same way.
//
// yaml.v3 ignores json struct tags and lowercases the Go field name
// instead, so without the round-trip in Encode the caller reads
// "knowledgeobjects" under yaml and "knowledge_objects" under json for
// one and the same record.
func TestEncodeYAMLUsesJSONFieldNames(t *testing.T) {
	withFormat(t, "yaml")

	type jobs struct {
		Total int `json:"total"`
	}
	// Field order here is chosen to satisfy govet's fieldalignment; the
	// json tags, not the declaration order, are what the assertions below
	// are about.
	type payload struct {
		DefaultProfile   string `json:"default_profile,omitempty"`
		Dropped          string `json:"-"`
		Jobs             jobs   `json:"jobs"`
		KnowledgeObjects int    `json:"knowledge_objects"`
	}

	var buf bytes.Buffer
	if err := Encode(&buf, payload{
		KnowledgeObjects: 4,
		Jobs:             jobs{Total: 2},
		DefaultProfile:   "demo",
		Dropped:          "must not appear",
	}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out := buf.String()

	for _, key := range []string{"knowledge_objects: 4", "total: 2", "default_profile: demo"} {
		if !strings.Contains(out, key) {
			t.Errorf("yaml should carry %q, got:\n%s", key, out)
		}
	}
	// The lowercased-field-name spelling is the bug this guards.
	if strings.Contains(out, "knowledgeobjects") {
		t.Errorf("yaml must honor the json tag, not the Go field name, got:\n%s", out)
	}
	// json:"-" means omitted, in both encodings.
	if strings.Contains(out, "must not appear") {
		t.Errorf(`a json:"-" field must stay out of the yaml document, got:\n%s`, out)
	}
}

// TestEncodeYAMLKeepsIntegersExact guards the UseNumber decode in
// jsonShaped. A plain untyped json decode routes every number through
// float64, which silently rounds an int64 past 2^53 and renders it in
// scientific notation — so an ID or a nanosecond timestamp would come
// back as a different number than the one the command reported.
func TestEncodeYAMLKeepsIntegersExact(t *testing.T) {
	withFormat(t, "yaml")

	var buf bytes.Buffer
	if err := Encode(&buf, map[string]any{
		"nano_ts": int64(1758300000123456789),
		"total":   7,
		"ratio":   1.5,
	}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "nano_ts: 1758300000123456789") {
		t.Errorf("a large int64 must survive byte-exact, got:\n%s", out)
	}
	// An integer must not acquire a fractional part or exponent, and a
	// number must not be quoted into a string.
	if !strings.Contains(out, "total: 7") {
		t.Errorf("an integer should render as a bare integer, got:\n%s", out)
	}
	if !strings.Contains(out, "ratio: 1.5") {
		t.Errorf("a float should keep its value, got:\n%s", out)
	}
	if strings.Contains(out, `"7"`) || strings.Contains(out, "e+") {
		t.Errorf("numbers must stay unquoted scalars in decimal form, got:\n%s", out)
	}
}

func TestActiveDefaultsToTable(t *testing.T) {
	withFormat(t, "")
	if got := Active(); got != Table {
		t.Errorf("an unset --format should resolve to %q, got %q", Table, got)
	}
}
