// Package blobref spells the blob:// refs content above
// storage.blob.threshold is stored as. It imports nothing, so packages
// that must recognise a ref without reaching a blob store (the index
// projection) can.
package blobref

import "strings"

// Prefix starts every blob ref; the blob key follows it.
const Prefix = "blob://"

// MetaTextTruncated flags an externalized object whose text_content is
// a prefix of its content rather than all of it. A reader that resolves
// the content gets the whole text back from it.
const MetaTextTruncated = "text_content_truncated"

// Is reports whether s is a blob ref.
func Is(s string) bool {
	return strings.HasPrefix(s, Prefix) && len(s) > len(Prefix)
}

// Key returns the blob key s refers to, and whether s is a ref.
func Key(s string) (string, bool) {
	if !Is(s) {
		return "", false
	}
	return s[len(Prefix):], true
}

// For returns the ref for blob key key.
func For(key string) string { return Prefix + key }
