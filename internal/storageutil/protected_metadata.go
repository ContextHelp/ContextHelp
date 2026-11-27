package storageutil

import "maps"

// protectedMetadataKeys are the metadata keys no pipeline step may set
// or change on a stored object: the caller's structured data and the
// ambient-capture envelope. They are capture-side data that no step
// regenerates, so a rewrite of the object (a reanalysis) restores them
// from the stored object after its steps ran. Later keys join here.
var protectedMetadataKeys = append([]string{MetadataDataKey}, ambientMetadataKeys...)

// RestoreProtectedMetadata sets the protected keys of md to their values
// in stored, removing a protected key stored does not hold, and returns
// md (a new map when md is nil and a key is restored). The restored values
// are copies: a later change to either map leaves the other alone.
func RestoreProtectedMetadata(md, stored map[string]any) map[string]any {
	for _, k := range protectedMetadataKeys {
		v, ok := stored[k]
		if !ok {
			delete(md, k)
			continue
		}
		if md == nil {
			md = map[string]any{}
		}
		md[k] = cloneValue(v)
	}
	return md
}

// CloneMetadata returns a deep copy of md: nested maps and slices are
// copied, so a step that edits them in place cannot reach md.
func CloneMetadata(md map[string]any) map[string]any {
	if md == nil {
		return nil
	}
	out := make(map[string]any, len(md))
	for k, v := range md {
		out[k] = cloneValue(v)
	}
	return out
}

func cloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return CloneMetadata(t)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = cloneValue(e)
		}
		return out
	case []string:
		return append([]string(nil), t...)
	case map[string]string:
		return maps.Clone(t)
	default:
		return v
	}
}
