package storageutil

import (
	"os"
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
	dsn := os.Getenv("POSTGRES_URL")
	if dsn == "" {
		t.Skip("POSTGRES_URL not set; skipping postgres driver test")
	}
	driver, err := NewDriver("postgres", dsn)
	if err != nil {
		t.Fatalf("new postgres driver: %v", err)
	}
	if driver == nil {
		t.Fatal("driver is nil")
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
