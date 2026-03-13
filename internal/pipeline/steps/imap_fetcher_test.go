package steps

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeIMAPServer implements a minimal IMAP4rev1 server for testing.
// It serves a plain-text (no TLS) session with pre-scripted message bodies.
type fakeIMAPServer struct {
	listener net.Listener
}

func newFakeIMAPServer(t *testing.T, messages []string) (*fakeIMAPServer, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &fakeIMAPServer{listener: ln}
	go srv.serveOne(messages)
	return srv, ln.Addr().String()
}

func (s *fakeIMAPServer) serveOne(messages []string) {
	conn, err := s.listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	defer s.listener.Close()

	conn.SetDeadline(time.Now().Add(5 * time.Second))
	r := bufio.NewReader(conn)

	readTag := func() string {
		line, _ := r.ReadString('\n')
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			return ""
		}
		return fields[0]
	}

	// Greeting.
	fmt.Fprintf(conn, "* OK IMAP4rev1 test server ready\r\n")

	// LOGIN
	loginTag := readTag()
	if loginTag == "" {
		return
	}
	fmt.Fprintf(conn, "%s OK LOGIN completed\r\n", loginTag)

	// SELECT
	selectTag := readTag()
	if selectTag == "" {
		return
	}
	fmt.Fprintf(conn, "* OK [UIDVALIDITY 1000] UIDs valid\r\n")
	fmt.Fprintf(conn, "* %d EXISTS\r\n", len(messages))
	fmt.Fprintf(conn, "%s OK SELECT completed\r\n", selectTag)

	// SEARCH
	searchTag := readTag()
	if searchTag == "" {
		return
	}
	if len(messages) == 0 {
		fmt.Fprintf(conn, "* SEARCH\r\n")
	} else {
		uids := make([]string, len(messages))
		for i := range messages {
			uids[i] = fmt.Sprintf("%d", i+1)
		}
		fmt.Fprintf(conn, "* SEARCH %s\r\n", strings.Join(uids, " "))
	}
	fmt.Fprintf(conn, "%s OK SEARCH completed\r\n", searchTag)

	if len(messages) == 0 {
		// LOGOUT
		logoutTag := readTag()
		if logoutTag != "" {
			fmt.Fprintf(conn, "%s OK LOGOUT completed\r\n", logoutTag)
		}
		return
	}

	// FETCH
	fetchTag := readTag()
	if fetchTag == "" {
		return
	}
	for i, msg := range messages {
		fmt.Fprintf(conn, "* %d FETCH (UID %d RFC822 {%d}\r\n%s)\r\n", i+1, i+1, len(msg), msg)
	}
	fmt.Fprintf(conn, "%s OK FETCH completed\r\n", fetchTag)

	// LOGOUT
	logoutTag := readTag()
	if logoutTag != "" {
		fmt.Fprintf(conn, "%s OK LOGOUT completed\r\n", logoutTag)
	}
}

func TestIMAPFetcherParseUIDValidity(t *testing.T) {
	tests := []struct {
		line string
		want uint32
	}{
		{"* OK [UIDVALIDITY 1234567890] UIDs valid", 1234567890},
		{"* OK [UIDVALIDITY 1000] UIDs valid", 1000},
		{"* 5 EXISTS", 0},
		{"A002 OK SELECT completed", 0},
	}
	for _, tt := range tests {
		got := parseUIDValidity([]string{tt.line})
		if got != tt.want {
			t.Errorf("parseUIDValidity(%q): got %d, want %d", tt.line, got, tt.want)
		}
	}
}

func TestIMAPFetcherParseSearchUIDs(t *testing.T) {
	lines := []string{
		"* SEARCH 1 2 3 42",
		"A003 OK SEARCH completed",
	}
	uids := parseSearchUIDs(lines)
	if len(uids) != 4 {
		t.Fatalf("expected 4 UIDs, got %d", len(uids))
	}
	if uids[3] != 42 {
		t.Errorf("uid[3]: got %d, want 42", uids[3])
	}
}

func TestIMAPFetcherParseEmptySearch(t *testing.T) {
	lines := []string{
		"* SEARCH",
		"A003 OK SEARCH completed",
	}
	uids := parseSearchUIDs(lines)
	if len(uids) != 0 {
		t.Errorf("expected 0 UIDs, got %d: %v", len(uids), uids)
	}
}

func TestIMAPFetcherBuildUIDSet(t *testing.T) {
	uids := []uint32{1, 2, 3}
	result := buildUIDSet(uids)
	if result != "1,2,3" {
		t.Errorf("got %q, want 1,2,3", result)
	}
	if buildUIDSet(nil) != "" {
		t.Error("expected empty string for nil input")
	}
}

func TestIMAPFetcherConfigDefaults(t *testing.T) {
	cfg := IMAPConfig{
		Host:     "imap.example.com",
		Username: "user",
		Password: "pass",
	}
	f := NewIMAPFetcher(cfg)
	if f.Name() != "imap_fetcher" {
		t.Errorf("Name(): got %q", f.Name())
	}
}

// TestIMAPFetcherInternalFetch tests the internal fetchMessages via a fake server
// that simulates a plain IMAP session (no TLS). The fetcher uses an internal helper
// that accepts a pre-connected imapConn to allow bypassing the TLS negotiation in tests.
func TestIMAPFetcherInternalFetch(t *testing.T) {
	msg1 := "From: alice@example.com\r\nTo: bob@example.com\r\nSubject: Test 1\r\nMessage-Id: <t1@example.com>\r\nDate: Mon, 01 Jan 2024 10:00:00 +0000\r\n\r\nBody one.\r\n"

	_, addr := newFakeIMAPServer(t, []string{msg1})

	host, portStr, _ := net.SplitHostPort(addr)
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	cfg := IMAPConfig{
		Host:     host,
		Port:     port,
		Username: "user",
		Password: "pass",
		UseTLS:   false,
	}

	// Dial directly and wrap with imapConn to bypass the connect() STARTTLS flow.
	rawConn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer rawConn.Close()
	rawConn.SetDeadline(time.Now().Add(5 * time.Second))

	result, err := fetchMessagesOnConn(rawConn, cfg)
	if err != nil {
		t.Fatalf("fetchMessagesOnConn: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(result.Messages))
	}
	if result.UIDValidity != 1000 {
		t.Errorf("UIDValidity: got %d, want 1000", result.UIDValidity)
	}
	if result.LastUID != 1 {
		t.Errorf("LastUID: got %d, want 1", result.LastUID)
	}
}

// TestIMAPFetcherEmptyMailboxOnConn tests the empty mailbox case.
func TestIMAPFetcherEmptyMailboxOnConn(t *testing.T) {
	_, addr := newFakeIMAPServer(t, nil)

	rawConn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer rawConn.Close()
	rawConn.SetDeadline(time.Now().Add(5 * time.Second))

	cfg := IMAPConfig{
		Host:     "localhost",
		Username: "user",
		Password: "pass",
		UseTLS:   false,
	}
	result, err := fetchMessagesOnConn(rawConn, cfg)
	if err != nil {
		t.Fatalf("fetchMessagesOnConn: %v", err)
	}
	if len(result.Messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(result.Messages))
	}
}
