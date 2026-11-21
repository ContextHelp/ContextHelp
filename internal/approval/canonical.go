package approval

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// DigestPrefix starts every payload digest and names its version: the
// canonical form below, hashed with SHA-256.
const DigestPrefix = "v1:sha256:"

// Canonicalize returns raw in canonical form: one JSON value with object
// keys sorted, no insignificant whitespace, no HTML escaping and
// numbers kept as written. Equal values in any key order or spacing
// canonicalize to the same bytes, so their digests match.
func Canonicalize(raw json.RawMessage) (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("canonical json: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("canonical json: trailing data after the value")
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	// encoding/json writes map keys sorted.
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("canonical json: %w", err)
	}
	return json.RawMessage(bytes.TrimSuffix(buf.Bytes(), []byte("\n"))), nil
}

// Digest returns the payload digest of a canonical payload.
func Digest(canonical json.RawMessage) string {
	sum := sha256.Sum256(canonical)
	return DigestPrefix + hex.EncodeToString(sum[:])
}
