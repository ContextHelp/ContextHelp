package steps

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// IMAPConfig holds configuration for an IMAP4rev1 connection.
type IMAPConfig struct {
	Host     string // hostname or host:port
	Port     int    // defaults to 993 for TLS, 143 for STARTTLS
	Username string
	Password string
	Folder   string // mailbox/folder to fetch (default: INBOX)
	UseTLS   bool   // true = implicit TLS (port 993); false = STARTTLS (port 143)
	// Cursor for incremental sync.
	UIDValidity uint32 `json:"uid_validity,omitempty"`
	LastUID     uint32 `json:"last_uid,omitempty"`
	// Limits.
	MaxMessages int // 0 = unlimited
}

// IMAPFetchResult holds the fetched messages and updated cursor state.
type IMAPFetchResult struct {
	Messages    []string // raw RFC 5322 message strings
	UIDValidity uint32
	LastUID     uint32
}

// IMAPFetcher fetches messages from an IMAP4rev1 mailbox and stores them as
// draft.RawContent (mbox format) or sets draft.Metadata["email_raw_messages"].
// It updates the cursor in draft.Metadata["imap_cursor"].
type IMAPFetcher struct {
	pipeline.BaseContract
	cfg    IMAPConfig
	dialer func(network, addr string) (net.Conn, error)
}

// IMAPFetcherOption configures an IMAPFetcher.
type IMAPFetcherOption func(*IMAPFetcher)

// WithIMAPDialer replaces the TCP dialer (for testing).
func WithIMAPDialer(d func(network, addr string) (net.Conn, error)) IMAPFetcherOption {
	return func(f *IMAPFetcher) { f.dialer = d }
}

// NewIMAPFetcher creates an IMAPFetcher.
func NewIMAPFetcher(cfg IMAPConfig, opts ...IMAPFetcherOption) *IMAPFetcher {
	f := &IMAPFetcher{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{},
			Produces: []string{"RawContent", "Metadata"},
		}),
		cfg: cfg,
	}
	for _, o := range opts {
		o(f)
	}
	return f
}

func (s *IMAPFetcher) Name() string { return "imap_fetcher" }

func (s *IMAPFetcher) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}

	// Load cursor from metadata if not already set in config.
	cfg := s.cfg
	if rawCursor, ok := draft.Metadata["imap_cursor"]; ok {
		if cursor, ok := rawCursor.(map[string]any); ok {
			if v, ok := cursor["uid_validity"].(uint32); ok && cfg.UIDValidity == 0 {
				cfg.UIDValidity = v
			}
			if v, ok := cursor["last_uid"].(uint32); ok && cfg.LastUID == 0 {
				cfg.LastUID = v
			}
		}
	}

	result, err := s.fetchMessages(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("imap_fetcher: %w", err)
	}

	// Store raw messages for the email_parser step.
	draft.Metadata["email_raw_messages"] = result.Messages
	draft.Metadata["imap_fetched_count"] = len(result.Messages)
	draft.Metadata["imap_cursor"] = map[string]any{
		"uid_validity": result.UIDValidity,
		"last_uid":     result.LastUID,
	}

	// Build mbox-format RawContent for downstream email_parser.
	var sb strings.Builder
	for _, raw := range result.Messages {
		sb.WriteString("From imap@fetcher " + time.Now().UTC().Format(time.ANSIC) + "\n")
		sb.WriteString(raw)
		sb.WriteString("\n\n")
	}
	draft.RawContent = sb.String()

	return draft, nil
}

// fetchMessages connects to the IMAP server and retrieves new messages.
func (s *IMAPFetcher) fetchMessages(ctx context.Context, cfg IMAPConfig) (*IMAPFetchResult, error) {
	conn, err := s.connect(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()
	return fetchMessagesOnConn(conn, cfg)
}

// fetchMessagesOnConn performs the IMAP session on an already-established connection.
// This function is extracted to enable testing without TLS negotiation.
func fetchMessagesOnConn(conn net.Conn, cfg IMAPConfig) (*IMAPFetchResult, error) {
	c := newIMAPConn(conn)

	// Read server greeting.
	if _, err := c.readLine(); err != nil {
		return nil, fmt.Errorf("greeting: %w", err)
	}

	// LOGIN
	if err := c.cmd("A001", fmt.Sprintf("LOGIN %q %q", cfg.Username, cfg.Password)); err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	if _, err := c.readUntilTagged("A001"); err != nil {
		return nil, fmt.Errorf("login response: %w", err)
	}

	// SELECT mailbox
	folder := cfg.Folder
	if folder == "" {
		folder = "INBOX"
	}
	if err := c.cmd("A002", fmt.Sprintf("SELECT %q", folder)); err != nil {
		return nil, fmt.Errorf("select: %w", err)
	}
	selectLines, err := c.readUntilTagged("A002")
	if err != nil {
		return nil, fmt.Errorf("select response: %w", err)
	}

	// Parse UIDVALIDITY from SELECT response.
	uidValidity := parseUIDValidity(selectLines)

	// If UIDVALIDITY changed, reset cursor.
	if cfg.UIDValidity != 0 && uidValidity != cfg.UIDValidity {
		cfg.LastUID = 0
	}

	// UID SEARCH for messages newer than cursor.
	searchTag := "A003"
	var searchCriteria string
	if cfg.LastUID > 0 {
		searchCriteria = fmt.Sprintf("UID SEARCH UID %d:*", cfg.LastUID+1)
	} else {
		searchCriteria = "UID SEARCH ALL"
	}
	if err := c.cmd(searchTag, searchCriteria); err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	searchLines, err := c.readUntilTagged(searchTag)
	if err != nil {
		return nil, fmt.Errorf("search response: %w", err)
	}

	uids := parseSearchUIDs(searchLines)
	if len(uids) == 0 {
		// LOGOUT before returning.
		_ = c.cmd("A005", "LOGOUT")
		return &IMAPFetchResult{
			UIDValidity: uidValidity,
			LastUID:     cfg.LastUID,
		}, nil
	}

	// Apply max limit.
	if cfg.MaxMessages > 0 && len(uids) > cfg.MaxMessages {
		uids = uids[:cfg.MaxMessages]
	}

	// UID FETCH messages.
	uidSet := buildUIDSet(uids)
	fetchTag := "A004"
	if err := c.cmd(fetchTag, fmt.Sprintf("UID FETCH %s (RFC822)", uidSet)); err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	fetchLines, err := c.readUntilTagged(fetchTag)
	if err != nil {
		return nil, fmt.Errorf("fetch response: %w", err)
	}

	messages := parseRFC822Messages(fetchLines)

	var lastUID uint32
	if len(uids) > 0 {
		lastUID = uids[len(uids)-1]
	}

	// LOGOUT
	_ = c.cmd("A005", "LOGOUT")

	return &IMAPFetchResult{
		Messages:    messages,
		UIDValidity: uidValidity,
		LastUID:     lastUID,
	}, nil
}

// newIMAPTLSConfig builds the TLS client config for both the implicit-TLS
// (port 993) and STARTTLS (port 143) paths.
//
// MinVersion is pinned to TLS 1.2. Go's client default still negotiates
// down to TLS 1.0, which carries known-broken ciphers; 1.2 is the lowest
// version worth speaking to a mail server. TLS 1.3 is deliberately NOT
// the floor: plenty of production IMAP servers still terminate at 1.2,
// and raising the floor that far would turn a security hardening into an
// outage for those accounts. 1.3 is still negotiated when offered.
func newIMAPTLSConfig(host string) *tls.Config {
	return &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	}
}

// connect establishes a TCP connection (TLS or plain for STARTTLS).
func (s *IMAPFetcher) connect(ctx context.Context, cfg IMAPConfig) (net.Conn, error) {
	host := cfg.Host
	port := cfg.Port
	if port == 0 {
		if cfg.UseTLS {
			port = 993
		} else {
			port = 143
		}
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	dial := s.dialer
	if dial == nil {
		d := &net.Dialer{Timeout: 30 * time.Second}
		dial = func(network, addr string) (net.Conn, error) {
			return d.DialContext(ctx, network, addr)
		}
	}

	conn, err := dial("tcp", addr)
	if err != nil {
		return nil, err
	}

	if cfg.UseTLS {
		tlsCfg := newIMAPTLSConfig(host)
		conn = tls.Client(conn, tlsCfg)
		if err := conn.(*tls.Conn).Handshake(); err != nil {
			conn.Close()
			return nil, fmt.Errorf("TLS handshake: %w", err)
		}
		return conn, nil
	}

	// STARTTLS: read greeting, send STARTTLS, upgrade.
	c := newIMAPConn(conn)
	if _, err := c.readLine(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("greeting: %w", err)
	}
	if err := c.cmd("S001", "STARTTLS"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("STARTTLS command: %w", err)
	}
	lines, err := c.readUntilTagged("S001")
	if err != nil || !containsOK(lines) {
		conn.Close()
		return nil, fmt.Errorf("STARTTLS response: not OK")
	}
	tlsConn := tls.Client(conn, newIMAPTLSConfig(host))
	if err := tlsConn.Handshake(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("STARTTLS handshake: %w", err)
	}
	// Return a wrapper that redirects reads through the new TLS connection.
	return tlsConn, nil
}

// imapConn wraps a net.Conn with buffered IMAP line reading.
type imapConn struct {
	conn net.Conn
	r    *bufio.Reader
	w    *bufio.Writer
}

func newIMAPConn(conn net.Conn) *imapConn {
	return &imapConn{
		conn: conn,
		r:    bufio.NewReader(conn),
		w:    bufio.NewWriter(conn),
	}
}

func (c *imapConn) cmd(tag, command string) error {
	line := tag + " " + command + "\r\n"
	if _, err := c.w.WriteString(line); err != nil {
		return err
	}
	return c.w.Flush()
}

func (c *imapConn) readLine() (string, error) {
	line, err := c.r.ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
}

// readUntilTagged reads IMAP response lines until a tagged response for tag is found.
func (c *imapConn) readUntilTagged(tag string) ([]string, error) {
	var lines []string
	for {
		line, err := c.r.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if err != nil && err != io.EOF {
			return lines, err
		}
		lines = append(lines, line)

		// Check for literal {n} continuation.
		if strings.HasSuffix(line, "}") {
			idx := strings.LastIndex(line, "{")
			if idx >= 0 {
				sizeStr := line[idx+1 : len(line)-1]
				size, parseErr := strconv.Atoi(sizeStr)
				if parseErr == nil && size > 0 {
					literal := make([]byte, size)
					if _, readErr := io.ReadFull(c.r, literal); readErr == nil {
						lines = append(lines, string(literal))
					}
					continue
				}
			}
		}

		// Detect tagged response: "tag OK|NO|BAD ..."
		if strings.HasPrefix(line, tag+" ") {
			return lines, nil
		}
		if err == io.EOF {
			return lines, io.EOF
		}
	}
}

// --- IMAP response parsers ---

func parseUIDValidity(lines []string) uint32 {
	for _, line := range lines {
		upper := strings.ToUpper(line)
		if strings.Contains(upper, "UIDVALIDITY") {
			// e.g. "* OK [UIDVALIDITY 1234567890] UIDs valid"
			idx := strings.Index(upper, "UIDVALIDITY")
			rest := strings.TrimSpace(line[idx+len("UIDVALIDITY"):])
			// rest may be " 1234567890] ..." or "1234567890]..."
			rest = strings.TrimLeft(rest, " \t")
			// Extract numeric portion before any ']' or space.
			end := strings.IndexAny(rest, "] \t\r\n")
			if end >= 0 {
				rest = rest[:end]
			}
			if v, err := strconv.ParseUint(rest, 10, 32); err == nil {
				return uint32(v)
			}
		}
	}
	return 0
}

func parseSearchUIDs(lines []string) []uint32 {
	var uids []uint32
	for _, line := range lines {
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "* SEARCH") {
			parts := strings.Fields(line)
			for _, p := range parts[2:] {
				v, err := strconv.ParseUint(p, 10, 32)
				if err == nil {
					uids = append(uids, uint32(v))
				}
			}
		}
	}
	return uids
}

// parseRFC822Messages extracts raw message content from FETCH responses.
func parseRFC822Messages(lines []string) []string {
	var messages []string
	i := 0
	for i < len(lines) {
		line := lines[i]
		upper := strings.ToUpper(line)
		if strings.Contains(upper, "RFC822") && strings.Contains(line, "{") {
			// The literal body is the next element.
			i++
			if i < len(lines) {
				messages = append(messages, lines[i])
			}
		}
		i++
	}
	return messages
}

func buildUIDSet(uids []uint32) string {
	if len(uids) == 0 {
		return ""
	}
	parts := make([]string, len(uids))
	for i, uid := range uids {
		parts[i] = strconv.FormatUint(uint64(uid), 10)
	}
	return strings.Join(parts, ",")
}

func containsOK(lines []string) bool {
	for _, line := range lines {
		if strings.Contains(strings.ToUpper(line), " OK") {
			return true
		}
	}
	return false
}
