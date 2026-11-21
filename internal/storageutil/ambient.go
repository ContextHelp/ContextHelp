package storageutil

import "github.com/ideacrafterslabs/ctxt/internal/storage"

// Object metadata keys the ambient-capture envelope lands under
// (ADR-066). Server-owned: client metadata never replaces them.
const (
	MetadataAmbientSourceKey = "ambient_source"
	MetadataFingerprintKey   = "fingerprint"
)

// reservedMetadataKeys are the keys client metadata may not set: the
// ambient keys above, the caller data namespace, and the keys that steer
// the pipeline or the content hash (next_pipeline hops, forge identity).
var reservedMetadataKeys = map[string]bool{
	MetadataAmbientSourceKey: true,
	MetadataFingerprintKey:   true,
	MetadataDataKey:          true,
	"next_pipeline":          true,
	"forge":                  true,
	"forge_id":               true,
}

// ApplyAmbient lands the ambient-capture context a:
//
//   - a.Source and a.Fingerprint become metadata keys ambient_source and
//     fingerprint;
//   - a.SessionID becomes obj.SessionID;
//   - a.Metadata merges into obj.Metadata key by key. Precedence: server
//     wins. A key already on the object, and every reserved key, keeps
//     its server value; client metadata only adds keys.
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
	for k, v := range a.Metadata {
		if reservedMetadataKeys[k] {
			continue
		}
		if _, set := obj.Metadata[k]; set {
			continue
		}
		obj.Metadata[k] = v
	}
	if len(obj.Metadata) == 0 {
		obj.Metadata = nil
	}
}
