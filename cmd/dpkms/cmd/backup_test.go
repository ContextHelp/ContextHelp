package cmd

import (
	"strings"
	"testing"
)

func TestBackupCommandRegistered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Use == "backup" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("backup command not registered on rootCmd")
	}
}

func TestBackupCommandFlags(t *testing.T) {
	flags := []string{"include-blobs", "output-dir", "async", "skip-blob-errors"}
	for _, flag := range flags {
		if backupCmd.Flags().Lookup(flag) == nil {
			t.Errorf("backup command missing flag --%s", flag)
		}
	}
}

func TestBackupCommandHelp(t *testing.T) {
	out, err := executeCommand("backup", "--help")
	if err != nil {
		t.Fatalf("backup --help should succeed: %v", err)
	}
	for _, s := range []string{"backup", "--include-blobs", "--output-dir", "--async"} {
		if !strings.Contains(out, s) {
			t.Errorf("backup --help should contain %q, got: %s", s, out)
		}
	}
}
