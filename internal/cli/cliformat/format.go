// Package cliformat centralizes how ctxt and dpkms read, validate and
// honor the global --format flag.
//
// kit/console/cli registers --format on the root and kit/console/output
// owns the registry of formatter keys (csv, human, json, table, text,
// yaml). Both binaries previously read the flag ad hoc, accepted any
// string, and rendered the human table regardless — a value the caller
// asked for was silently discarded under a success exit code.
//
// This package is the single place where that resolution happens:
//
//   - Validate rejects a key kit does not know, echoing the offending
//     value and naming the valid set, as a kit UsageError so the
//     process exits with the canonical usage code.
//   - Structured reports whether the active format is a machine format,
//     replacing the old json-only predicate.
//   - Encode renders a value in the active machine format.
//
// Callers read the format through Active rather than viper so the
// unset case resolves consistently: kit reports an unset --format as
// "table", and an empty string means the same thing.
package cliformat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
	"hop.top/kit/go/console/output"
)

// Table is the key kit reports for an unset --format.
const Table = "table"

// Markdown and MarkdownShort are the prose-document spellings some
// commands (notably `resolve`) accept alongside kit's registry keys.
// They render human prose, not a machine document, so Structured is
// false for them.
const (
	Markdown      = "markdown"
	MarkdownShort = "md"
)

// IsMarkdown reports whether format names one of the markdown
// spellings.
func IsMarkdown(format string) bool {
	return format == Markdown || format == MarkdownShort
}

// activeCmd is the command currently executing, published by Bind so
// Active can consult the real cobra flag. It is nil outside a run.
var activeCmd *cobra.Command

// Bind records the executing command for the duration of the run. Call
// it from the root's persistent pre-run hook, before any validation.
func Bind(cmd *cobra.Command) { activeCmd = cmd }

// Active returns the format key currently in effect, normalised so an
// unset flag reads as Table rather than "".
//
// A --format set on the command line wins over viper. This mirrors
// kit's own lookupStringFlag precedence and matters beyond style: the
// in-process test harnesses call viper.Set("format", "") to scrub
// state between cases, and an explicit viper.Set outranks a bound
// pflag, so reading viper alone would report "" for a run that did
// pass --format json.
func Active() string {
	if f := changedFormatFlag(); f != "" {
		return f
	}
	f := strings.TrimSpace(viper.GetString("format"))
	if f == "" {
		return Table
	}
	return f
}

// changedFormatFlag returns the --format value when it was explicitly
// set on the command line.
//
// It reads the ROOT's persistent flag set only. kit registers --format
// there, and that single set is what both binaries' in-process test
// harnesses reset between cases. A leaf's own Flags() is a lazily
// merged copy that the harness never clears, so consulting it would
// let one test's --format leak into the next.
func changedFormatFlag() string {
	if activeCmd == nil {
		return ""
	}
	pf := activeCmd.Root().PersistentFlags().Lookup("format")
	if pf == nil || !pf.Changed {
		return ""
	}
	return strings.TrimSpace(pf.Value.String())
}

// Valid returns every format key the binaries accept — kit's registry
// keys plus the markdown spellings — sorted, for help text and error
// messages.
func Valid() []string {
	keys := append(output.Default.Keys(), Markdown, MarkdownShort)
	sort.Strings(keys)
	return keys
}

// Validate reports whether format names a formatter kit knows about.
//
// An unknown value is a caller mistake, not a runtime failure, so the
// returned error is a kit UsageError: it pins the canonical usage exit
// code and carries a message that both echoes the rejected value and
// lists every accepted one, so an agent can correct itself without
// consulting the docs.
func Validate(format string) error {
	if strings.TrimSpace(format) == "" {
		return nil
	}
	if _, ok := output.Default.Lookup(format); ok {
		return nil
	}
	if IsMarkdown(format) {
		return nil
	}
	return output.UsageError(fmt.Sprintf(
		"unknown --format %q (valid: %s)",
		format, strings.Join(Valid(), ", ")))
}

// ValidateActive validates whatever --format currently resolves to.
func ValidateActive() error { return Validate(Active()) }

// Structured reports whether the active format is a machine-readable
// document rather than the human table. Commands branch on this to
// choose between Encode and their human renderer.
func Structured() bool { return IsStructured(Active()) }

// IsStructured reports whether format names a machine-readable
// document format.
func IsStructured(format string) bool {
	switch format {
	case output.JSON, output.YAML:
		return true
	default:
		return false
	}
}

// Encode writes v to w in the active machine format.
//
// Nil and nil-valued slices and maps are normalised to their empty
// counterparts first: a caller iterating result.jobs must receive []
// rather than null when nothing matched. Without this an empty
// collection is indistinguishable from a missing field on the wire.
func Encode(w io.Writer, v any) error {
	v = normalizeEmpty(v)
	if Active() == output.YAML {
		doc, err := jsonShaped(v)
		if err != nil {
			return err
		}
		enc := yaml.NewEncoder(w)
		encErr := enc.Encode(doc)
		// Close flushes the trailing document, so it runs either way; a
		// failure there means the caller got a truncated document. The
		// encode error is the more specific one, so it wins.
		closeErr := enc.Close()
		if encErr != nil {
			return encErr
		}
		return closeErr
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// jsonShaped re-decodes v through its JSON representation so the YAML
// document carries exactly the field names the JSON document does.
//
// The commands in both binaries declare their payload shapes with
// json struct tags only. yaml.v3 does not read those tags: it derives
// a key by lowercasing the Go field name, so a KnowledgeObjects field
// tagged knowledge_objects reaches a YAML caller as
// "knowledgeobjects". Two documents that are supposed to be two
// encodings of one record end up disagreeing about what the fields are
// called, and a caller cannot switch --format json for --format yaml
// without rewriting every path it reads.
//
// Round-tripping through encoding/json is what makes the tags
// authoritative for both: json.Marshal applies them, and unmarshalling
// into `any` yields the plain maps, slices, strings and float64s that
// yaml.v3 renders without consulting any tags at all. Adding parallel
// yaml struct tags to every payload struct would work too, but it
// leaves two spellings to keep in sync on every future field and
// silently regresses the moment one is forgotten — the failure this
// function exists to remove.
//
// The decode runs with UseNumber, which is load-bearing rather than
// incidental. A plain untyped json.Unmarshal turns every number into a
// float64, and a float64 cannot hold an int64 past 2^53: a nanosecond
// timestamp round-trips as 1758300000123456768, and yaml.v3 renders
// large values in scientific notation besides. Deferring the decision
// to json.Number keeps the literal digits, and numeric restores each
// one to the narrowest type that represents it exactly.
func jsonShaped(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("cliformat: encode %T for yaml: %w", v, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("cliformat: decode %T for yaml: %w", v, err)
	}
	return numeric(doc), nil
}

// numeric replaces every json.Number left by the UseNumber decode with
// a real Go number, so yaml.v3 renders it as a scalar.
//
// Without this a count reaches the caller as the string "7": yaml.v3
// has no special handling for json.Number, sees a named string type,
// and quotes it. An integer is preferred when the literal fits one
// exactly, which is what keeps `total: 7` from becoming `total: 7.0`
// and keeps IDs and timestamps byte-exact. A literal that is neither
// an exact integer nor a representable float — only a number too large
// for float64 reaches this — keeps its digits as a string rather than
// being silently rounded.
func numeric(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, item := range x {
			x[k] = numeric(item)
		}
		return x
	case []any:
		for i, item := range x {
			x[i] = numeric(item)
		}
		return x
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i
		}
		if f, err := x.Float64(); err == nil {
			return f
		}
		return x.String()
	}
	return v
}

// normalizeEmpty replaces nil slices and maps with empty ones so they
// serialize as [] and {} instead of null. It recurses one level into
// maps and slices, which covers the shapes these CLIs emit: a envelope
// map whose values are the collections, or a bare collection.
//
// Structs are returned untouched: rewriting their fields would need a
// deep copy via reflection on unexported state, and the empty-null
// problem in practice lives in the map[string]any envelopes and the
// top-level slices, not in struct fields the commands construct.
func normalizeEmpty(v any) any {
	if v == nil {
		return []any{}
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice:
		if rv.IsNil() {
			return []any{}
		}
	case reflect.Map:
		if rv.IsNil() {
			return map[string]any{}
		}
		// Normalise each value so {"jobs": null} becomes {"jobs": []}.
		if m, ok := v.(map[string]any); ok {
			out := make(map[string]any, len(m))
			for k, val := range m {
				out[k] = normalizeEmptyLeaf(val)
			}
			return out
		}
	case reflect.Ptr, reflect.Interface:
		if rv.IsNil() {
			return map[string]any{}
		}
	}
	return v
}

// normalizeEmptyLeaf normalizes a single map value without recursing
// further, so a nil slice nested in an envelope serializes as [].
//
// A nil interface stays nil: an absent value is genuinely null, and
// only a nil slice or map carries the "empty collection" meaning that
// must serialize as [] or {}.
func normalizeEmptyLeaf(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice:
		if rv.IsNil() {
			return []any{}
		}
	case reflect.Map:
		if rv.IsNil() {
			return map[string]any{}
		}
	}
	return v
}
