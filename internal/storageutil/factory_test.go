package storageutil

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNewDriverSQLite(t *testing.T) {
	dir := t.TempDir()
	driver, err := NewDriver("sqlite", filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("new driver: %v", err)
	}
	if driver == nil {
		t.Fatal("driver is nil")
	}
}

func TestNewDriverPostgres(t *testing.T) {
	_, err := NewDriver("postgres", "")
	if err == nil {
		t.Fatal("expected error for postgres")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("error: got %q", err)
	}
}

func TestNewDriverUnknown(t *testing.T) {
	_, err := NewDriver("foo", "")
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
	if !strings.Contains(err.Error(), "unknown storage type") {
		t.Errorf("error: got %q", err)
	}
}
