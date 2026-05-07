package cmd

import (
	"testing"
)

func TestIngestHelp(t *testing.T) {
	out, err := executeCommand("ingest", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--source", "--stdin", "--every"} {
		if !containsStr(out, want) {
			t.Errorf("help missing %q", want)
		}
	}
}

func TestIngestRequiresSource(t *testing.T) {
	_, err := executeCommand("ingest")
	if err == nil {
		t.Fatal("expected error when --source missing")
	}
}

func TestIngestUnknownAdapter(t *testing.T) {
	_, err := executeCommand("ingest", "--source", "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown adapter")
	}
}

func TestIngestRegistryHasCardamum(t *testing.T) {
	reg := IngestRegistry()
	names := reg.List()
	found := false
	for _, n := range names {
		if n == "cardamum" {
			found = true
			break
		}
	}
	if !found {
		t.Error("cardamum adapter not registered")
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
