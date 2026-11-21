package storageutil

import (
	"maps"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Object metadata keys the ambient-capture envelope lands under
// (ADR-066). ambient_source and fingerprint are set only by the server
// from the typed request fields; client metadata is isolated under
// MetadataClientKey and never read by server or pipeline code.
const (
	MetadataAmbientSourceKey = "ambient_source"
	MetadataFingerprintKey   = "fingerprint"
	MetadataClientKey        = "client"
)

// ambientMetadataKeys are the object metadata keys that hold the ambient
// envelope: kept through a reanalysis, withheld from sharing and
// federation projections.
var ambientMetadataKeys = []string{MetadataAmbientSourceKey, MetadataFingerprintKey, MetadataClientKey}

// ApplyAmbient lands the ambient-capture context a on obj:
//
//   - a.Source and a.Fingerprint become metadata keys ambient_source and
//     fingerprint;
//   - a.SessionID becomes obj.SessionID;
//   - a.Metadata is stored whole, as a map, under metadata.client. It is
//     never flat-merged: a client key can therefore not collide with a
//     key a step, the worker or the store reads (suppress_output,
//     duplicate_of, blob_key, items_to_enqueue, title, ...).
//
// A nil or empty a changes nothing.
func ApplyAmbient(obj *storage.KnowledgeObject, a *storage.AmbientContext) {
	if a.Empty() {
		return
	}
	if obj.Metadata == nil {
		obj.Metadata = map[string]any{}
	}
	if a.Source != "" {
		obj.Metadata[MetadataAmbientSourceKey] = a.Source
	}
	if a.Fingerprint != "" {
		obj.Metadata[MetadataFingerprintKey] = a.Fingerprint
	}
	if a.SessionID != "" {
		obj.SessionID = a.SessionID
	}
	if len(a.Metadata) > 0 {
		obj.Metadata[MetadataClientKey] = maps.Clone(a.Metadata)
	}
	if len(obj.Metadata) == 0 {
		obj.Metadata = nil
	}
}

// CarryAmbient copies the ambient envelope of from (session id and the
// ambient metadata keys) onto to, for a rewrite of the same object that
// rebuilds its metadata, such as a reanalysis.
func CarryAmbient(to, from *storage.KnowledgeObject) {
	if from.SessionID != "" {
		to.SessionID = from.SessionID
	}
	for _, k := range ambientMetadataKeys {
		if v, ok := from.Metadata[k]; ok {
			if to.Metadata == nil {
				to.Metadata = map[string]any{}
			}
			to.Metadata[k] = v
		}
	}
}

// WithoutAmbient returns md without the ambient envelope keys; md is not
// modified. For projections that leave the instance (sharing,
// federation): the fingerprint and the client's own metadata are
// capture-side data, not content to share.
func WithoutAmbient(md map[string]any) map[string]any {
	if md == nil {
		return nil
	}
	out := maps.Clone(md)
	for _, k := range ambientMetadataKeys {
		delete(out, k)
	}
	return out
}
