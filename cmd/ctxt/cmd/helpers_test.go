package cmd

import (
	"bytes"
	"testing"
)

func TestPrintTable(t *testing.T) {
	var buf bytes.Buffer
	printTable(&buf, []string{"ID", "Title"}, [][]string{
		{"obj-1", "First object"},
		{"obj-2", "Second object"},
	})
	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("ID")) {
		t.Fatalf("expected header 'ID' in output: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("obj-1")) {
		t.Fatalf("expected 'obj-1' in output: %s", out)
	}
}

func TestOutputJSON(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]string{"key": "value"}
	if err := outputJSON(&buf, data); err != nil {
		t.Fatalf("outputJSON: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"key"`)) {
		t.Fatalf("expected JSON key in output: %s", buf.String())
	}
}

func TestIsJSONOutput(t *testing.T) {
	// isJSONOutput reads viper "output.format"
	// Default should be false (text)
	if isJSONOutput() {
		t.Fatal("expected text output by default")
	}
}
