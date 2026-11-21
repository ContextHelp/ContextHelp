// Package contentenc is the content_encoding of an analyze or enqueue
// request: how the content string on the wire maps to the bytes captured.
//
// A JSON string carries UTF-8 only, and Go's encoder replaces every invalid
// sequence with U+FFFD, so raw file bytes (a PNG, a PDF) cannot travel as
// the content string. A sender base64-encodes content that is not valid
// UTF-8 and says so with content_encoding; text travels as it always did.
package contentenc

import (
	"encoding/base64"
	"errors"
	"fmt"
	"unicode/utf8"
)

// Base64 is the content_encoding of standard-alphabet, padded base64.
// The empty encoding is the default: content is UTF-8 text, as sent.
const Base64 = "base64"

// ErrUnknown marks a content_encoding this server does not support.
var ErrUnknown = errors.New("unknown content_encoding")

// ErrInvalidBase64 marks content that is not valid base64.
var ErrInvalidBase64 = errors.New("content is not valid base64")

// Encode returns the content and content_encoding to send for s: s
// unchanged with no encoding when it is valid UTF-8, base64 otherwise.
func Encode(s string) (content, encoding string) {
	if utf8.ValidString(s) {
		return s, ""
	}
	return base64.StdEncoding.EncodeToString([]byte(s)), Base64
}

// EncodeBytes is Encode for bytes.
func EncodeBytes(b []byte) (content, encoding string) {
	if utf8.Valid(b) {
		return string(b), ""
	}
	return base64.StdEncoding.EncodeToString(b), Base64
}

// Decode returns the bytes content stands for under encoding. An unknown
// encoding wraps ErrUnknown, malformed base64 wraps ErrInvalidBase64.
func Decode(content, encoding string) (string, error) {
	switch encoding {
	case "":
		return content, nil
	case Base64:
		b, err := base64.StdEncoding.DecodeString(content)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrInvalidBase64, err)
		}
		return string(b), nil
	default:
		return "", fmt.Errorf("%w %q (supported: base64)", ErrUnknown, clip(encoding))
	}
}

// maxEncodingEcho caps how much of a caller-supplied encoding an error
// repeats back.
const maxEncodingEcho = 32

func clip(s string) string {
	if len(s) <= maxEncodingEcho {
		return s
	}
	cut := s[:maxEncodingEcho]
	for !utf8.ValidString(cut) && len(cut) > 0 {
		cut = cut[:len(cut)-1]
	}
	return cut + "..."
}
