package steps

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// EmailMessage is the normalized representation of a parsed email message.
type EmailMessage struct {
	MessageID   string            `json:"message_id"`
	Subject     string            `json:"subject"`
	From        string            `json:"from"`
	FromDomain  string            `json:"from_domain"`
	To          []string          `json:"to"`
	CC          []string          `json:"cc"`
	ReplyTo     string            `json:"reply_to,omitempty"`
	ListID      string            `json:"list_id,omitempty"`
	Date        time.Time         `json:"date"`
	TextBody    string            `json:"text_body"`
	HTMLBody    string            `json:"html_body"`
	Attachments []AttachmentMeta  `json:"attachments,omitempty"`
	Headers     map[string]string `json:"headers"`
	SizeBytes   int               `json:"size_bytes"`
	ContentHash string            `json:"content_hash"`
	// CharsetGuessed is set when a charset was assumed or undecodable
	// bytes were replaced while reading the message.
	CharsetGuessed bool `json:"charset_guessed,omitempty"`
}

// AttachmentMeta captures attachment metadata without storing content.
type AttachmentMeta struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int    `json:"size_bytes,omitempty"`
}

// EmailParser parses raw .eml content or mbox streams from draft.RawContent.
// It produces email message maps in draft.Metadata["email_messages"].
type EmailParser struct {
	pipeline.BaseContract
	maxBodyBytes int
}

// EmailParserOption configures an EmailParser.
type EmailParserOption func(*EmailParser)

// WithEmailMaxBodyBytes limits the body size extracted per message.
func WithEmailMaxBodyBytes(n int) EmailParserOption {
	return func(p *EmailParser) { p.maxBodyBytes = n }
}

// NewEmailParser creates an EmailParser with optional configuration.
func NewEmailParser(opts ...EmailParserOption) *EmailParser {
	p := &EmailParser{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Binary:   true,
			Requires: []string{"RawContent"},
			Produces: []string{"Metadata"},
		}),
		maxBodyBytes: 1 << 20, // 1 MiB default
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

func (s *EmailParser) Name() string { return "email_parser" }

func (s *EmailParser) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	raw := draft.RawContent
	if raw == "" {
		draft.Metadata["email_messages"] = []map[string]any{}
		return draft, nil
	}

	var msgs []EmailMessage
	var parseErr error

	// Messages are bytes, not necessarily UTF-8: a legacy 8-bit message
	// is read by the charsets it declares. A message whose header block
	// holds NUL is not mail; parseEML refuses it, and an mbox skips it.
	// Detect mbox format: starts with "From " envelope line.
	trimmed := strings.TrimLeft(raw, "\r\n")
	if strings.HasPrefix(trimmed, "From ") {
		msgs, parseErr = parseMbox(trimmed, s.maxBodyBytes)
	} else {
		var msg EmailMessage
		msg, parseErr = parseEML(raw, s.maxBodyBytes)
		if parseErr == nil {
			msgs = []EmailMessage{msg}
		}
	}

	if parseErr != nil {
		return nil, pipeline.Permanent(fmt.Errorf("email_parser: %w", parseErr))
	}

	result := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		result = append(result, emailToMap(m))
		if m.CharsetGuessed {
			draft.Metadata[MetaCharsetGuessed] = true
		}
	}
	draft.Metadata["email_messages"] = result
	draft.Metadata["email_count"] = len(result)
	return draft, nil
}

// parseMbox splits mbox content and parses each RFC 5322 message; one
// that does not parse is skipped. Content with no message that parses is
// an error, so a container never completes with nothing in it.
func parseMbox(content string, maxBodyBytes int) ([]EmailMessage, error) {
	var messages []EmailMessage
	var firstErr error
	var current strings.Builder
	inMsg := false

	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "From ") {
			if inMsg && current.Len() > 0 {
				msg, err := parseEML(current.String(), maxBodyBytes)
				if err == nil {
					messages = append(messages, msg)
				} else if firstErr == nil {
					firstErr = err
				}
				current.Reset()
			}
			inMsg = true
			// Skip the mbox envelope line (not an RFC 5322 header).
			continue
		}
		if inMsg {
			current.WriteString(line)
			current.WriteByte('\n')
		}
	}
	// Last message.
	if inMsg && current.Len() > 0 {
		msg, err := parseEML(current.String(), maxBodyBytes)
		if err == nil {
			messages = append(messages, msg)
		} else if firstErr == nil {
			firstErr = err
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read mbox: %w", err)
	}
	if len(messages) == 0 {
		if firstErr == nil {
			firstErr = fmt.Errorf("no message")
		}
		return nil, fmt.Errorf("mbox holds no parsable message: %w", firstErr)
	}
	return messages, nil
}

// parseEML parses a single RFC 5322 message.
func parseEML(raw string, maxBodyBytes int) (EmailMessage, error) {
	var msg EmailMessage
	msg.SizeBytes = len(raw)
	msg.Headers = make(map[string]string)

	if strings.IndexByte(headerBlock(raw), 0) >= 0 {
		return msg, fmt.Errorf("header block holds NUL bytes: not an email")
	}
	m, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		return msg, fmt.Errorf("parse RFC5322: %w", err)
	}

	// Each header is decoded on its own: 8-bit bytes that are not UTF-8 in
	// one header do not change how the others read.
	h := m.Header
	for _, vs := range h {
		for i, v := range vs {
			c, g := headerText(v)
			vs[i] = c
			msg.CharsetGuessed = msg.CharsetGuessed || g
		}
	}

	msg.MessageID = cleanEmailHeader(h.Get("Message-Id"))
	msg.Subject = decodeEmailHeader(h.Get("Subject"))
	msg.ListID = cleanEmailHeader(h.Get("List-Id"))
	msg.ReplyTo = cleanEmailHeader(h.Get("Reply-To"))

	if fromAddr, err := mail.ParseAddress(h.Get("From")); err == nil {
		msg.From = fromAddr.Address
		parts := strings.SplitN(fromAddr.Address, "@", 2)
		if len(parts) == 2 {
			msg.FromDomain = strings.ToLower(parts[1])
		}
	} else {
		msg.From = cleanEmailHeader(h.Get("From"))
	}

	if addrs, err := m.Header.AddressList("To"); err == nil {
		for _, a := range addrs {
			msg.To = append(msg.To, a.Address)
		}
	}
	if addrs, err := m.Header.AddressList("Cc"); err == nil {
		for _, a := range addrs {
			msg.CC = append(msg.CC, a.Address)
		}
	}

	if t, err := m.Header.Date(); err == nil {
		msg.Date = t
	}

	// Content-Hash over Message-ID + Subject + From for dedup.
	h256 := sha256.New()
	fmt.Fprintf(h256, "%s\x00%s\x00%s", msg.MessageID, msg.Subject, msg.From)
	msg.ContentHash = fmt.Sprintf("%x", h256.Sum(nil))

	// Store a subset of headers for the filter engine.
	for _, key := range []string{
		"Subject", "From", "To", "Cc", "Reply-To", "List-Id",
		"X-Mailer", "X-Spam-Flag", "Content-Type",
	} {
		if v := h.Get(key); v != "" {
			msg.Headers[strings.ToLower(key)] = v
		}
	}

	// Parse body.
	ct := h.Get("Content-Type")
	if ct == "" {
		ct = mediaTextPlain
	}
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		mediaType = mediaTextPlain
		params = map[string]string{}
	}

	bodyBytes, _ := io.ReadAll(m.Body)
	if len(bodyBytes) > maxBodyBytes {
		bodyBytes = trimToRuneBoundary(bodyBytes[:maxBodyBytes])
	}

	// Decode top-level transfer encoding.
	topCTE := strings.ToLower(h.Get("Content-Transfer-Encoding"))
	bodyBytes = emailDecodeCTE(topCTE, bodyBytes)

	extractEmailBodies(mediaType, params, bodyBytes, &msg)
	return msg, nil
}

// headerBlock is raw up to the blank line that ends its headers.
func headerBlock(raw string) string {
	end := len(raw)
	for _, sep := range []string{"\r\n\r\n", "\n\n"} {
		if i := strings.Index(raw, sep); i >= 0 && i < end {
			end = i
		}
	}
	return raw[:end]
}

// trimToRuneBoundary cuts b back so it does not end inside a multi-byte
// UTF-8 sequence, which a size cut can leave and which would otherwise
// read as an invalid byte.
func trimToRuneBoundary(b []byte) []byte {
	for back := 1; back <= utf8.UTFMax && back <= len(b); back++ {
		c := b[len(b)-back]
		if c < utf8.RuneSelf {
			return b
		}
		if utf8.RuneStart(c) {
			if r, size := utf8.DecodeRune(b[len(b)-back:]); r == utf8.RuneError && size <= 1 && !utf8.FullRune(b[len(b)-back:]) {
				return b[:len(b)-back]
			}
			return b
		}
	}
	return b
}

// extractEmailBodies recursively walks multipart structures to collect
// bodies and attachments. Only text parts (text/*, or any part declaring
// a charset) are decoded into bodies; every other media type, inline or
// not, is recorded as an attachment. A part is decoded from the charset
// it declares, else read as undeclared text. A malformed part (a message
// cut inside a part header) ends the walk of its multipart and keeps the
// parts already read.
func extractEmailBodies(mediaType string, params map[string]string, bodyBytes []byte, msg *EmailMessage) {
	switch {
	case strings.HasPrefix(mediaType, "multipart/"):
		boundary := params["boundary"]
		if boundary == "" {
			return
		}
		mr := multipart.NewReader(bytes.NewReader(bodyBytes), boundary)
		for {
			part, err := mr.NextPart()
			if err != nil {
				if !errors.Is(err, io.EOF) {
					slog.Debug("email_parser: malformed multipart part, keeping the parts read", "err", err)
				}
				break
			}
			partBytes, err := io.ReadAll(part)
			// ReadAll drained the part, so Close has nothing left to read
			// or report; the header stays readable after it.
			_ = part.Close()
			if err != nil {
				continue
			}
			partCT, g := headerText(part.Header.Get("Content-Type"))
			msg.CharsetGuessed = msg.CharsetGuessed || g
			if partCT == "" {
				partCT = mediaTextPlain
			}
			partMedia, partParams, err := mime.ParseMediaType(partCT)
			if err != nil {
				partMedia = mediaTextPlain
				partParams = map[string]string{}
			}

			// Decode part transfer encoding.
			partCTE := strings.ToLower(part.Header.Get("Content-Transfer-Encoding"))
			decoded := emailDecodeCTE(partCTE, partBytes)

			partDisp, g := headerText(part.Header.Get("Content-Disposition"))
			msg.CharsetGuessed = msg.CharsetGuessed || g
			var dispType string
			var dispParams map[string]string
			if partDisp != "" {
				dispType, dispParams, _ = mime.ParseMediaType(partDisp)
			}

			if strings.EqualFold(dispType, "attachment") || !isBodyPart(partMedia, partParams) {
				filename := dispParams["filename"]
				if filename == "" {
					filename = partParams["name"]
				}
				msg.Attachments = append(msg.Attachments, AttachmentMeta{
					Filename:    decodeEmailHeader(filename),
					ContentType: cleanText(partMedia),
					SizeBytes:   len(partBytes),
				})
				continue
			}

			extractEmailBodies(partMedia, partParams, decoded, msg)
		}

	case !isBodyPart(mediaType, params):
		msg.Attachments = append(msg.Attachments, AttachmentMeta{
			Filename:    decodeEmailHeader(params["name"]),
			ContentType: cleanText(mediaType),
			SizeBytes:   len(bodyBytes),
		})

	case strings.HasPrefix(mediaType, mediaTextHTML):
		if msg.HTMLBody == "" {
			msg.HTMLBody = decodeBody(bodyBytes, params["charset"], msg)
		}

	default: // text/plain or another text type
		if msg.TextBody == "" {
			msg.TextBody = decodeBody(bodyBytes, params["charset"], msg)
		}
	}
}

// isBodyPart reports whether a part is text a body is read from: a text/*
// or multipart/* media type, or any type that declares a charset. Other
// media types (images, archives, documents) are never decoded as text.
func isBodyPart(mediaType string, params map[string]string) bool {
	return strings.HasPrefix(mediaType, "text/") ||
		strings.HasPrefix(mediaType, "multipart/") ||
		params["charset"] != ""
}

// decodeBody decodes a text part to clean UTF-8 (see decodeText) and
// notes on msg when a charset was assumed or bytes replaced. A NUL inside
// text is dropped, not a reason to lose the message.
func decodeBody(data []byte, charset string, msg *EmailMessage) string {
	out, guessed := decodeText(data, charset)
	if strings.IndexByte(out, 0) >= 0 {
		out, guessed = dropNUL(out), true
	}
	msg.CharsetGuessed = msg.CharsetGuessed || guessed
	return out
}

// emailDecodeCTE decodes quoted-printable or base64 transfer encoding.
func emailDecodeCTE(cte string, data []byte) []byte {
	switch cte {
	case "quoted-printable":
		decoded, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(data)))
		if err == nil {
			return decoded
		}
	case "base64":
		// Strip whitespace before decoding.
		stripped := bytes.Map(func(r rune) rune {
			if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, data)
		dst := make([]byte, base64.StdEncoding.DecodedLen(len(stripped)))
		n, err := base64.StdEncoding.Decode(dst, stripped)
		if err == nil {
			return dst[:n]
		}
		// Try RawStdEncoding as fallback (no padding).
		n, err = base64.RawStdEncoding.Decode(dst, stripped)
		if err == nil {
			return dst[:n]
		}
	}
	return data
}

// cleanEmailHeader removes surrounding angle brackets and whitespace.
func cleanEmailHeader(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "<>")
	return s
}

// decodeEmailHeader decodes RFC 2047 encoded-word syntax.
func decodeEmailHeader(s string) string {
	dec := mime.WordDecoder{CharsetReader: charsetReader}
	decoded, err := dec.DecodeHeader(s)
	if err != nil {
		return cleanText(s)
	}
	return cleanText(decoded)
}

// emailToMap converts an EmailMessage to a map[string]any for metadata storage.
func emailToMap(m EmailMessage) map[string]any {
	attachments := make([]map[string]any, 0, len(m.Attachments))
	for _, a := range m.Attachments {
		attachments = append(attachments, map[string]any{
			"filename":     a.Filename,
			"content_type": a.ContentType,
			"size_bytes":   a.SizeBytes,
		})
	}
	toList := make([]any, len(m.To))
	for i, addr := range m.To {
		toList[i] = addr
	}
	ccList := make([]any, len(m.CC))
	for i, addr := range m.CC {
		ccList[i] = addr
	}
	headers := make(map[string]any, len(m.Headers))
	for k, v := range m.Headers {
		headers[k] = v
	}

	out := map[string]any{
		"message_id":   m.MessageID,
		"subject":      m.Subject,
		"from":         m.From,
		"from_domain":  m.FromDomain,
		"to":           toList,
		"cc":           ccList,
		"reply_to":     m.ReplyTo,
		"list_id":      m.ListID,
		"date":         m.Date.Format(time.RFC3339),
		"text_body":    m.TextBody,
		"html_body":    m.HTMLBody,
		"attachments":  attachments,
		"headers":      headers,
		"size_bytes":   m.SizeBytes,
		"content_hash": m.ContentHash,
	}
	if m.CharsetGuessed {
		out[MetaCharsetGuessed] = true
	}
	return out
}
