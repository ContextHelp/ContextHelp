package storage

// Metadata keys of the artifact node that stands for a stored original
// (ADR-076 §3). They live here, below the blob store, so packages that only
// read the keys (projection, representation) need not import it; the blob
// package re-exports them as blob.Meta*.
const (
	ArtifactMetaBlobKey     = "blob_key"
	ArtifactMetaContentType = "content_type"
	ArtifactMetaSizeBytes   = "size_bytes"
	ArtifactMetaSHA256      = "sha256"
	ArtifactMetaFileName    = "file_name"
)
