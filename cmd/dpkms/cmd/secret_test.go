package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSecretSetRejectsPositionalValue verifies that `dpkms secret set <key> <value>`
// is refused: the second positional arg leaks the secret via shell history
// and ps(1). Convention §7.3 requires stdin / --from-file / --prompt instead.
func TestSecretSetRejectsPositionalValue(t *testing.T) {
	_, err := readSecretValue([]string{"MYKEY", "sk-leaky"}, "", false, os.Stdin)
	if err == nil {
		t.Fatal("expected positional secret value to be rejected, got nil error")
	}
	if !strings.Contains(err.Error(), "must come from stdin") {
		t.Errorf("error should explain why CLI-arg values are refused, got: %v", err)
	}
	if !strings.Contains(err.Error(), "shell history") {
		t.Errorf("error should mention shell-history leak, got: %v", err)
	}
}

func TestSecretSetMissingKeyRejected(t *testing.T) {
	_, err := readSecretValue(nil, "", false, os.Stdin)
	if err == nil {
		t.Fatal("expected missing key to be rejected")
	}
	if !strings.Contains(err.Error(), "<key>") {
		t.Errorf("error should mention <key>, got: %v", err)
	}
}

func TestSecretSetFromFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(p, []byte("super-secret\n"), 0o600); err != nil {
		t.Fatalf("write secret file: %v", err)
	}

	got, err := readSecretValue([]string{"MYKEY"}, p, false, os.Stdin)
	if err != nil {
		t.Fatalf("readSecretValue: %v", err)
	}
	if string(got) != "super-secret" {
		t.Errorf("got %q, want %q (trailing newline should be trimmed)", got, "super-secret")
	}
}

func TestSecretSetFromFileEmptyRejected(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "empty.txt")
	if err := os.WriteFile(p, []byte{}, 0o600); err != nil {
		t.Fatalf("write empty file: %v", err)
	}
	_, err := readSecretValue([]string{"MYKEY"}, p, false, os.Stdin)
	if err == nil || !strings.Contains(err.Error(), "empty secret") {
		t.Errorf("expected empty-secret rejection, got: %v", err)
	}
}

func TestSecretSetMutuallyExclusive(t *testing.T) {
	_, err := readSecretValue([]string{"MYKEY"}, "/some/path", true, os.Stdin)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected mutually-exclusive rejection, got: %v", err)
	}
}

// TestSecretSetCommandHelpDocumentsSafeSources verifies the command's help
// text explains the new sources so users discover them.
func TestSecretSetCommandHelpDocumentsSafeSources(t *testing.T) {
	out, err := executeCommand("secret", "set", "--help")
	if err != nil {
		t.Fatalf("secret set --help: %v", err)
	}
	for _, want := range []string{"--from-file", "--prompt", "stdin"} {
		if !strings.Contains(out, want) {
			t.Errorf("secret set --help should mention %q, got:\n%s", want, out)
		}
	}
}

// TestSecretSetReadsFromStdinPipe simulates the `printf | dpkms secret set KEY` flow.
func TestSecretSetReadsFromStdinPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	if _, err := w.Write([]byte("piped-value\n")); err != nil {
		t.Fatalf("write to pipe: %v", err)
	}
	w.Close()

	got, err := readSecretValue([]string{"MYKEY"}, "", false, r)
	if err != nil {
		t.Fatalf("readSecretValue from stdin: %v", err)
	}
	if string(got) != "piped-value" {
		t.Errorf("got %q, want %q", got, "piped-value")
	}
}
