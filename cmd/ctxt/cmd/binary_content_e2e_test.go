package cmd

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/contentenc"
)

// binPNG is PNG-like: signature, NUL, bytes that are not valid UTF-8, and
// trailing whitespace a text trim would eat.
var binPNG = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0xff, 0xfe, 0x80, ' ', '\n'}

// runCLI runs the built ctxt binary against the fixture's server.
func (f *e2eFixture) runCLI(t *testing.T, stdin []byte, args ...string) (stdout, stderr string, exit int) {
	t.Helper()
	cmd := exec.Command(f.binary, append([]string{args[0], "--server", f.server.URL}, args[1:]...)...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	cmd.Env = append(os.Environ(), "CTXT_NO_CLIPBOARD=1")
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatal(err)
		}
		exit = ee.ExitCode()
	}
	return so.String(), se.String(), exit
}

// assertAnalyzeBodyCarries fails unless the one analyze POST the server
// saw names base64 and decodes to want.
func assertAnalyzeBodyCarries(t *testing.T, f *e2eFixture, want []byte) {
	t.Helper()
	recs := f.recordsFor("/api/v1/analyze")
	if len(recs) != 1 {
		t.Fatalf("analyze POSTs = %d, want 1", len(recs))
	}
	enc, _ := recs[0].Body["content_encoding"].(string)
	content, _ := recs[0].Body["content"].(string)
	got, err := contentenc.Decode(content, enc)
	if err != nil || enc != "base64" || !bytes.Equal([]byte(got), want) {
		t.Fatalf("content_encoding %q, decoded %x, err %v; want base64 %x", enc, got, err, want)
	}
}

func writeBin(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "img.png")
	if err := os.WriteFile(path, binPNG, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestE2ECaptureBinaryFileEncoded(t *testing.T) {
	f := newE2EFixture(t)
	if out, errOut, exit := f.runCLI(t, nil, "capture", writeBin(t)); exit != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, out, errOut)
	}
	assertAnalyzeBodyCarries(t, f, binPNG)
}

func TestE2ECaptureBinaryStdinEncoded(t *testing.T) {
	f := newE2EFixture(t)
	if out, errOut, exit := f.runCLI(t, binPNG, "capture", "--stdin"); exit != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, out, errOut)
	}
	assertAnalyzeBodyCarries(t, f, binPNG)
}

func TestE2ECaptureTextCarriesNoEncoding(t *testing.T) {
	f := newE2EFixture(t)
	if _, errOut, exit := f.runCLI(t, nil, "capture", "héllo"); exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, errOut)
	}
	recs := f.recordsFor("/api/v1/analyze")
	if _, ok := recs[0].Body["content_encoding"]; ok || recs[0].Body["content"] != "héllo" {
		t.Fatalf("body = %v, want plain text and no content_encoding", recs[0].Body)
	}
}

// The inbox endpoint takes no content_encoding, so binary is refused
// instead of reaching it as U+FFFD.
func TestE2ECaptureBinaryInboxRefused(t *testing.T) {
	f := newE2EFixture(t)
	_, errOut, exit := f.runCLI(t, nil, "capture", writeBin(t), "--inbox")
	if exit == 0 || !strings.Contains(errOut, "binary content is not supported with --inbox") {
		t.Fatalf("exit=%d stderr=%q; want a refusal", exit, errOut)
	}
	if n := len(f.recordsFor("/api/v1/inbox")); n != 0 {
		t.Fatalf("inbox POSTs = %d, want 0", n)
	}
}

func TestE2EAnalyzeBinaryFileEncoded(t *testing.T) {
	f := newE2EFixture(t)
	if out, errOut, exit := f.runCLI(t, nil, "analyze", "--file", writeBin(t)); exit != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, out, errOut)
	}
	assertAnalyzeBodyCarries(t, f, binPNG)
}

// analyze reads stdin through cli.GetInput: binary stays untrimmed and
// travels base64.
func TestE2EAnalyzeBinaryStdinEncoded(t *testing.T) {
	f := newE2EFixture(t)
	if out, errOut, exit := f.runCLI(t, binPNG, "analyze"); exit != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, out, errOut)
	}
	assertAnalyzeBodyCarries(t, f, binPNG)
}
