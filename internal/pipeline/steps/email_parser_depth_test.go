package steps

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

const depthTestHeader = "From: a@example.com\r\nTo: b@example.com\r\nSubject: Deep\r\nMessage-Id: <deep@example.com>\r\nDate: Mon, 01 Jan 2024 10:00:00 +0000\r\n"

// allocBound is generous: a walk bounded by maxEmailDepth copies a 1 MiB
// message a few dozen times, an unbounded one copies it once per level.
const allocBound = 64 << 20

func parseBounded(t *testing.T, raw string) EmailMessage {
	t.Helper()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	msg, err := parseEML(raw, 1<<20)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("parseEML: %v", err)
	}
	if got := after.TotalAlloc - before.TotalAlloc; got > allocBound {
		t.Fatalf("allocated %d MB parsing %d KB, want < %d MB", got>>20, len(raw)>>10, allocBound>>20)
	}
	return msg
}

func TestEmailParserNestedRFC822ChainIsBounded(t *testing.T) {
	const levels = 30000
	chain := "Subject: inner\r\n\r\ninnermost text\r\n"
	var b strings.Builder
	for i := 0; i < levels; i++ {
		b.WriteString("Content-Type: message/rfc822\r\n\r\n")
	}
	chain = b.String() + chain
	raw := depthTestHeader + "Content-Type: multipart/mixed; boundary=\"outer\"\r\n\r\n" +
		"--outer\r\nContent-Type: text/plain\r\n\r\nouter text\r\n" +
		"--outer\r\nContent-Type: message/rfc822\r\n\r\n" + chain + "\r\n--outer--\r\n"
	if len(raw) > 1<<20 {
		t.Fatalf("fixture %d bytes, want <= 1 MiB", len(raw))
	}

	msg := parseBounded(t, raw)
	if !strings.Contains(msg.TextBody, "outer text") {
		t.Errorf("outermost text lost: %q", msg.TextBody)
	}
}

func TestEmailParserNestedMultipartIsBounded(t *testing.T) {
	const levels = 8000
	var open, closing strings.Builder
	for i := 0; i < levels; i++ {
		fmt.Fprintf(&open, "--b%d\r\nContent-Type: multipart/mixed; boundary=\"b%d\"\r\n\r\n", i, i+1)
	}
	for i := levels - 1; i >= 0; i-- {
		fmt.Fprintf(&closing, "--b%d--\r\n", i)
	}
	raw := depthTestHeader + "Content-Type: multipart/mixed; boundary=\"b0\"\r\n\r\n" +
		"--b0\r\nContent-Type: text/plain\r\n\r\nouter text\r\n" + open.String() + closing.String()
	if len(raw) > 1<<20 {
		t.Fatalf("fixture %d bytes, want <= 1 MiB", len(raw))
	}

	msg := parseBounded(t, raw)
	if !strings.Contains(msg.TextBody, "outer text") {
		t.Errorf("outermost text lost: %q", msg.TextBody)
	}
}

func TestEmailParserDepthCapRecordsAttachment(t *testing.T) {
	// maxEmailDepth+3 rfc822 levels: the walk stops and the rest is an
	// attachment, the text above it kept.
	inner := "Subject: s\r\n\r\ndeep text\r\n"
	for i := 0; i < maxEmailDepth+3; i++ {
		inner = "Content-Type: message/rfc822\r\n\r\n" + inner
	}
	raw := depthTestHeader + inner
	msg, err := parseEML(raw, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range msg.Attachments {
		if a.ContentType == mediaMessageRFC822 {
			found = true
		}
	}
	if !found {
		t.Errorf("no message/rfc822 attachment past the depth cap: %+v", msg.Attachments)
	}
	if strings.Contains(msg.TextBody, "deep text") {
		t.Errorf("text past the depth cap was read: %q", msg.TextBody)
	}
}
