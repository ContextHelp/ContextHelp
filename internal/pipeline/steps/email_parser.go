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
	// is read by the charsets it declares. NUL bytes are not mail text.
	if strings.IndexByte(raw, 0) >= 0 {
		return nil, pipeline.Permanent(fmt.Errorf("email_parser: content holds NUL bytes: not an email"))
	}

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

	m, err := mail.ReadMessage(strings.NewReader(legacyHeaders(raw)))
	if err != nil {
		return msg, fmt.Errorf("parse RFC5322: %w", err)
	}

	h := m.Header

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
		bodyBytes = bodyBytes[:maxBodyBytes]
	}

	// Decode top-level transfer encoding.
	topCTE := strings.ToLower(h.Get("Content-Transfer-Encoding"))
	bodyBytes = emailDecodeCTE(topCTE, bodyBytes)

	if err := extractEmailBodies(mediaType, params, bodyBytes, &msg); err != nil {
		return msg, err
	}

	return msg, nil
}

// extractEmailBodies recursively walks multipart structures to collect bodies and attachments.
// A body is decoded from the charset its part declares; one that cannot
// be decoded is an error.
func extractEmailBodies(mediaType string, params map[string]string, bodyBytes []byte, msg *EmailMessage) error {
	switch {
	case strings.HasPrefix(mediaType, "multipart/"):
		boundary := params["boundary"]
		if boundary == "" {
			return nil
		}
		mr := multipart.NewReader(bytes.NewReader(bodyBytes), boundary)
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return fmt.Errorf("multipart part: %w", err)
			}
			partBytes, err := io.ReadAll(part)
			// ReadAll drained the part, so Close has nothing left to read
			// or report; the header stays readable after it.
			_ = part.Close()
			if err != nil {
				continue
			}
			partCT := part.Header.Get("Content-Type")
			if partCT == "" {
				partCT = mediaTextPlain
			}
			partMedia, partParams, err := mime.ParseMediaType(legacyText(partCT))
			if err != nil {
				partMedia = mediaTextPlain
				partParams = map[string]string{}
			}

			// Decode part transfer encoding.
			partCTE := strings.ToLower(part.Header.Get("Content-Transfer-Encoding"))
			decoded := emailDecodeCTE(partCTE, partBytes)

			// Check disposition for attachments.
			partDisp := legacyText(part.Header.Get("Content-Disposition"))
			if partDisp != "" {
				dispType, dispParams, _ := mime.ParseMediaType(partDisp)
				if strings.EqualFold(dispType, "attachment") {
					filename := dispParams["filename"]
					if filename == "" {
						filename = partParams["name"]
					}
					filename = decodeEmailHeader(filename)
					msg.Attachments = append(msg.Attachments, AttachmentMeta{
						Filename:    filename,
						ContentType: partMedia,
						SizeBytes:   len(partBytes),
					})
					continue
				}
			}

			if err := extractEmailBodies(partMedia, partParams, decoded, msg); err != nil {
				return err
			}
		}
		return nil

	case strings.HasPrefix(mediaType, mediaTextHTML):
		if msg.HTMLBody == "" {
			s, err := toUTF8(bodyBytes, params["charset"])
			if err != nil {
				return fmt.Errorf("html body: %w", err)
			}
			msg.HTMLBody = s
		}

	default: // text/plain or anything unrecognized
		if msg.TextBody == "" {
			s, err := toUTF8(bodyBytes, params["charset"])
			if err != nil {
				return fmt.Errorf("text body: %w", err)
			}
			msg.TextBody = s
		}
	}
	return nil
}

// legacyHeaders returns raw with its header block as valid UTF-8: 8-bit
// header bytes that are not UTF-8 are read as Windows-1252. The body is
// untouched; its parts declare their own charsets.
func legacyHeaders(raw string) string {
	end := len(raw)
	for _, sep := range []string{"\r\n\r\n", "\n\n"} {
		if i := strings.Index(raw, sep); i >= 0 && i < end {
			end = i
		}
	}
	if utf8.ValidString(raw[:end]) {
		return raw
	}
	return legacyText(raw[:end]) + raw[end:]
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
		return s
	}
	return decoded
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

	return map[string]any{
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
}
