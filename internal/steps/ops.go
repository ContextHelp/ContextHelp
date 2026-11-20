package steps

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// stepOp is one additive operation in a step's output (ADR-058,
// amendment 2026-11-19). Ops change one field relative to its current
// value, so steps need not echo the field to keep it.
type stepOp struct {
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value"`
}

// defaultOpTagSource labels tags a step adds as bare strings.
const defaultOpTagSource = "step"

// ApplyStepOps applies a step's ops, in order, to in. raw is the JSON
// array of ops. The input object is never mutated. Ops:
//
//	add_tags        value: array of label strings or tag objects
//	remove_tags     value: array of label strings
//	merge_metadata  value: object, merged as a JSON Merge Patch
//
// Errors name the op's position and kind, never its values.
func ApplyStepOps(in *storage.KnowledgeObject, raw json.RawMessage) (*storage.KnowledgeObject, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("invalid output: ops is not a JSON array")
	}
	out := *in
	for i, item := range items {
		var op stepOp
		dec := json.NewDecoder(bytes.NewReader(item))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&op); err != nil {
			return nil, fmt.Errorf("invalid output: ops[%d] is not an {op, value} object", i)
		}
		if op.Op == "" {
			return nil, fmt.Errorf(`invalid output: ops[%d]: missing "op"`, i)
		}
		var err error
		switch op.Op {
		case "add_tags":
			err = opAddTags(&out, op.Value)
		case "remove_tags":
			err = opRemoveTags(&out, op.Value)
		case "merge_metadata":
			err = opMergeMetadata(&out, op.Value)
		default:
			return nil, fmt.Errorf("invalid output: ops[%d]: unknown op %q; ops are add_tags, remove_tags, merge_metadata", i, op.Op)
		}
		if err != nil {
			return nil, fmt.Errorf("invalid output: ops[%d]: %s: %w", i, op.Op, err)
		}
	}
	return &out, nil
}

// tagKey is how tags compare: trimmed, case-insensitive (the tagger's
// dedup).
func tagKey(label string) string { return strings.ToLower(strings.TrimSpace(label)) }

func opAddTags(o *storage.KnowledgeObject, value json.RawMessage) error {
	var items []json.RawMessage
	if err := json.Unmarshal(value, &items); err != nil || items == nil {
		return fmt.Errorf("value must be an array of labels or tag objects")
	}
	tags := append([]storage.Tag(nil), o.Tags...)
	seen := make(map[string]struct{}, len(tags)+len(items))
	for _, t := range tags {
		seen[tagKey(t.Label)] = struct{}{}
	}
	for i, it := range items {
		var t storage.Tag
		var label string
		if err := json.Unmarshal(it, &label); err == nil {
			t = storage.Tag{Label: label, Weight: 1.0, Source: defaultOpTagSource}
		} else {
			dec := json.NewDecoder(bytes.NewReader(it))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&t); err != nil {
				return fmt.Errorf("element %d is neither a label string nor a {label, weight, source} object", i)
			}
		}
		t.Label = strings.TrimSpace(t.Label)
		if t.Label == "" {
			continue
		}
		key := tagKey(t.Label)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		tags = append(tags, t)
	}
	o.Tags = tags
	return nil
}

func opRemoveTags(o *storage.KnowledgeObject, value json.RawMessage) error {
	var items []json.RawMessage
	if err := json.Unmarshal(value, &items); err != nil || items == nil {
		return fmt.Errorf("value must be an array of label strings")
	}
	drop := make(map[string]struct{}, len(items))
	for i, it := range items {
		var l string
		if err := json.Unmarshal(it, &l); err != nil {
			return fmt.Errorf("element %d is not a label string", i)
		}
		drop[tagKey(l)] = struct{}{}
	}
	var tags []storage.Tag
	for _, t := range o.Tags {
		if _, ok := drop[tagKey(t.Label)]; !ok {
			tags = append(tags, t)
		}
	}
	o.Tags = tags
	return nil
}

func opMergeMetadata(o *storage.KnowledgeObject, value json.RawMessage) error {
	var patch map[string]any
	if err := json.Unmarshal(value, &patch); err != nil || patch == nil {
		return fmt.Errorf("value must be an object")
	}
	o.Metadata = mergePatch(o.Metadata, patch)
	return nil
}

// mergePatch applies patch to dst as RFC 7386 JSON Merge Patch and
// returns the result without mutating dst: objects merge recursively,
// anything else (arrays included) replaces, null deletes the key.
func mergePatch(dst, patch map[string]any) map[string]any {
	out := make(map[string]any, len(dst)+len(patch))
	for k, v := range dst {
		out[k] = v
	}
	for k, pv := range patch {
		if pv == nil {
			delete(out, k)
			continue
		}
		if pm, ok := pv.(map[string]any); ok {
			base, _ := out[k].(map[string]any)
			out[k] = mergePatch(base, pm)
			continue
		}
		out[k] = pv
	}
	return out
}
