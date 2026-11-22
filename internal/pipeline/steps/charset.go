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

// mostlyUTF8 reports whether data reads as UTF-8 with stray bytes (a
// truncated tail, a pasted Latin-1 character) rather than as a legacy
// charset: it holds at least one valid multi-byte UTF-8 sequence and no
// more invalid bytes than valid multi-byte ones. Legacy 8-bit text
// (Windows-1252, GBK, Shift-JIS) has no valid multi-byte sequence or is
// mostly invalid as UTF-8; ASCII with high bytes and no valid sequence is
// never mostly UTF-8, so it reads as Windows-1252.
func mostlyUTF8(data []byte) bool {
	var valid, invalid int
	for i := 0; i < len(data); {
		if data[i] < utf8.RuneSelf {
			i++
			continue
		}
		r, size := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && size == 1 {
			invalid++
		} else {
			valid += size
		}
		i += size
	}
	return valid > 0 && invalid <= valid
}

// undeclaredUTF8 reads data of no known charset as UTF-8 text. Valid
// UTF-8 is kept as it is. Text that is mostly UTF-8 keeps what is valid
// and has each invalid byte run replaced with U+FFFD, so one stray byte
// does not garble the rest. Anything else is read as Windows-1252, the
// legacy charset 8-bit text most often is. guessed reports that bytes
// were replaced or a charset was assumed.
func undeclaredUTF8(data []byte) (out string, guessed bool) {
	switch {
	case utf8.Valid(data):
		return string(data), false
	case mostlyUTF8(data):
		return strings.ToValidUTF8(string(data), string(utf8.RuneError)), true
	default:
		return decodeWindows1252(data), true
	}
}

// decodeText decodes data written in charset to valid UTF-8, never
// failing: a charset that is empty, unknown to the decoder (unknown-8bit,
// x-unknown), cannot decode data, or claims UTF-8 over invalid bytes is
// treated as undeclared (see undeclaredUTF8). guessed reports that the
// charset was assumed or bytes replaced. NUL bytes are left in place for
// the caller to refuse or drop.
func decodeText(data []byte, charset string) (out string, guessed bool) {
	if charset == "" {
		return undeclaredUTF8(data)
	}
	enc, err := htmlindex.Get(charset)
	if err != nil {
		out, _ = undeclaredUTF8(data)
		return out, true
	}
	if name, _ := htmlindex.Name(enc); name == "utf-8" {
		return undeclaredUTF8(data)
	}
	b, err := enc.NewDecoder().Bytes(data)
	if err != nil {
		return undeclaredUTF8(data)
	}
	return strings.ToValidUTF8(string(b), string(utf8.RuneError)), false
}

// toUTF8 is decodeText for content that must be text: one holding a NUL
// after decoding (a binary file, or UTF-16 without a declaration) is an
// error. The result is only ever valid UTF-8 free of NUL.
func toUTF8(data []byte, charset string) (string, bool, error) {
	out, guessed := decodeText(data, charset)
	if strings.IndexByte(out, 0) >= 0 {
		return "", false, fmt.Errorf("content holds NUL bytes after decoding: not text")
	}
	return out, guessed, nil
}

// dropNUL removes NUL bytes from s.
func dropNUL(s string) string {
	if strings.IndexByte(s, 0) < 0 {
		return s
	}
	return strings.ReplaceAll(s, "\x00", "")
}

// cleanText is s as text a database accepts: valid UTF-8 (invalid bytes
// become U+FFFD) without NUL.
func cleanText(s string) string {
	return dropNUL(strings.ToValidUTF8(s, string(utf8.RuneError)))
}

// headerText is one header value as clean UTF-8: valid UTF-8 stays, 8-bit
// bytes are decoded as undeclared text (see undeclaredUTF8), NUL is
// dropped. Each header is decided on its own, so one legacy header does
// not change how the others read.
func headerText(s string) (string, bool) {
	out, guessed := undeclaredUTF8([]byte(s))
	return dropNUL(out), guessed
}

func decodeWindows1252(data []byte) string {
	b, _ := charmap.Windows1252.NewDecoder().Bytes(data)
	return string(b)
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
// read as undeclared text, see undeclaredUTF8), and draft.Metadata gets
// charset_guessed when the charset was assumed or bytes were replaced.
// Content holding NUL after decoding is a permanent failure.
func textInput(step string, draft *storage.KnowledgeObject) (string, error) {
	raw := draft.RawContent
	if utf8.ValidString(raw) && strings.IndexByte(raw, 0) < 0 {
		return raw, nil
	}
	text, guessed, err := toUTF8([]byte(raw), declaredCharset(draft))
	if err != nil {
		return "", pipeline.Permanent(fmt.Errorf("%s: %w", step, err))
	}
	if guessed {
		markCharsetGuessed(draft)
	}
	return text, nil
}

// markCharsetGuessed records that a parser assumed a charset or replaced
// undecodable bytes in draft's content.
func markCharsetGuessed(draft *storage.KnowledgeObject) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata[MetaCharsetGuessed] = true
}

// MetaCharsetGuessed is the metadata key set when text was decoded with
// an assumed charset or had undecodable bytes replaced.
const MetaCharsetGuessed = "charset_guessed"
