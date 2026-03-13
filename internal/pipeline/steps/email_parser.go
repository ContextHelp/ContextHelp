package steps

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
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
		return nil, fmt.Errorf("email_parser: %w", parseErr)
	}

	result := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		result = append(result, emailToMap(m))
	}
	draft.Metadata["email_messages"] = result
	draft.Metadata["email_count"] = len(result)
	return draft, nil
}

// parseMbox splits mbox content and parses each RFC 5322 message.
func parseMbox(content string, maxBodyBytes int) ([]EmailMessage, error) {
	var messages []EmailMessage
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
		}
	}
	return messages, nil
}

// parseEML parses a single RFC 5322 message.
func parseEML(raw string, maxBodyBytes int) (EmailMessage, error) {
	var msg EmailMessage
	msg.SizeBytes = len(raw)
	msg.Headers = make(map[string]string)

	m, err := mail.ReadMessage(strings.NewReader(raw))
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
		ct = "text/plain"
	}
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		mediaType = "text/plain"
		params = map[string]string{}
	}

	bodyBytes, _ := io.ReadAll(m.Body)
	if len(bodyBytes) > maxBodyBytes {
		bodyBytes = bodyBytes[:maxBodyBytes]
	}

	// Decode top-level transfer encoding.
	topCTE := strings.ToLower(h.Get("Content-Transfer-Encoding"))
	bodyBytes = emailDecodeCTE(topCTE, bodyBytes)

	extractEmailBodies(mediaType, params, bodyBytes, &msg)

	return msg, nil
}

// extractEmailBodies recursively walks multipart structures to collect bodies and attachments.
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
				break
			}
			partBytes, err := io.ReadAll(part)
			if err != nil {
				part.Close()
				continue
			}
			partCT := part.Header.Get("Content-Type")
			if partCT == "" {
				partCT = "text/plain"
			}
			partMedia, partParams, err := mime.ParseMediaType(partCT)
			if err != nil {
				partMedia = "text/plain"
				partParams = map[string]string{}
			}

			// Decode part transfer encoding.
			partCTE := strings.ToLower(part.Header.Get("Content-Transfer-Encoding"))
			decoded := emailDecodeCTE(partCTE, partBytes)

			// Check disposition for attachments.
			partDisp := part.Header.Get("Content-Disposition")
			if partDisp != "" {
				dispType, dispParams, _ := mime.ParseMediaType(partDisp)
				if strings.EqualFold(dispType, "attachment") {
					filename := dispParams["filename"]
					if filename == "" {
						filename = partParams["name"]
					}
					msg.Attachments = append(msg.Attachments, AttachmentMeta{
						Filename:    filename,
						ContentType: partMedia,
						SizeBytes:   len(partBytes),
					})
					part.Close()
					continue
				}
			}

			extractEmailBodies(partMedia, partParams, decoded, msg)
			part.Close()
		}

	case strings.HasPrefix(mediaType, "text/html"):
		if msg.HTMLBody == "" {
			msg.HTMLBody = emailSafeString(bodyBytes)
		}

	default: // text/plain or anything unrecognized
		if msg.TextBody == "" {
			msg.TextBody = emailSafeString(bodyBytes)
		}
	}
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

// emailSafeString converts bytes to string, replacing invalid UTF-8 sequences.
func emailSafeString(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var buf strings.Builder
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		buf.WriteRune(r)
		b = b[size:]
	}
	return buf.String()
}

// cleanEmailHeader removes surrounding angle brackets and whitespace.
func cleanEmailHeader(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "<>")
	return s
}

// decodeEmailHeader decodes RFC 2047 encoded-word syntax.
func decodeEmailHeader(s string) string {
	dec := mime.WordDecoder{}
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
