package steps

import (
	"fmt"
	"io"
	"mime"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/htmlindex"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// charsetOf returns the charset parameter of a Content-Type value, or "".
func charsetOf(contentType string) string {
	if contentType == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(params["charset"])
}

// declaredCharset is the charset a draft declares: the fetched
// Content-Type's parameter, else the one on draft.ContentType.
func declaredCharset(draft *storage.KnowledgeObject) string {
	if ct, ok := draft.Metadata["content_type"].(string); ok {
		if cs := charsetOf(ct); cs != "" {
			return cs
		}
	}
	return charsetOf(draft.ContentType)
}

// toUTF8 decodes data written in charset to UTF-8. An empty charset
// leaves valid UTF-8 as it is and reads anything else as Windows-1252,
// the legacy charset 8-bit text most often is; a charset that claims
// UTF-8 over invalid bytes is read the same way. A charset the decoder
// does not know, or text holding a NUL after decoding (a binary file, or
// UTF-16 without a declaration), is an error: the result is only ever
// valid UTF-8 free of NUL.
func toUTF8(data []byte, charset string) (string, error) {
	var out string
	switch {
	case charset == "" && utf8.Valid(data):
		out = string(data)
	case charset == "":
		out = decodeWindows1252(data)
	default:
		enc, err := htmlindex.Get(charset)
		if err != nil {
			return "", fmt.Errorf("unsupported charset %q: %w", charset, err)
		}
		if name, _ := htmlindex.Name(enc); name == "utf-8" {
			if utf8.Valid(data) {
				out = string(data)
			} else {
				out = decodeWindows1252(data)
			}
			break
		}
		b, err := enc.NewDecoder().Bytes(data)
		if err != nil {
			return "", fmt.Errorf("decode charset %q: %w", charset, err)
		}
		out = string(b)
	}
	if strings.IndexByte(out, 0) >= 0 {
		return "", fmt.Errorf("content holds NUL bytes after decoding: not text")
	}
	return out, nil
}

func decodeWindows1252(data []byte) string {
	b, _ := charmap.Windows1252.NewDecoder().Bytes(data)
	return string(b)
}

// legacyText is s as valid UTF-8: text that already is stays, anything
// else is read as Windows-1252.
func legacyText(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return decodeWindows1252([]byte(s))
}

// charsetReader reads an RFC 2047 encoded word's charset for
// mime.WordDecoder.
func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	enc, err := htmlindex.Get(charset)
	if err != nil {
		return nil, err
	}
	return enc.NewDecoder().Reader(input), nil
}

// textInput is draft.RawContent as UTF-8 text for a parser: content
// that is not valid UTF-8 is decoded from its declared charset (else
// Windows-1252). Content that cannot be decoded is a permanent failure.
func textInput(step string, draft *storage.KnowledgeObject) (string, error) {
	raw := draft.RawContent
	if utf8.ValidString(raw) && strings.IndexByte(raw, 0) < 0 {
		return raw, nil
	}
	text, err := toUTF8([]byte(raw), declaredCharset(draft))
	if err != nil {
		return "", pipeline.Permanent(fmt.Errorf("%s: %w", step, err))
	}
	return text, nil
}
