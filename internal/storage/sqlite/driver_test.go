package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func newTestDriver(t testing.TB) *Driver {
	t.Helper()
	dir := t.TempDir()
	d, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("new driver: %v", err)
	}
	if err := d.Init(context.Background()); err != nil {
		t.Fatalf("init driver: %v", err)
	}
	t.Cleanup(func() { d.Close(context.Background()) })
	return d
}

func TestInit(t *testing.T) {
	dir := t.TempDir()
	d, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer d.Close(context.Background())

	if err := d.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
}

func TestHealth(t *testing.T) {
	d := newTestDriver(t)
	if err := d.Health(context.Background()); err != nil {
		t.Fatalf("health: %v", err)
	}
}

func TestClose(t *testing.T) {
	dir := t.TempDir()
	d, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := d.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := d.Close(context.Background()); err != nil {
		t.Fatalf("close: %v", err)
	}
}
