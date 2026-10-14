package service

import (
	"io"
	"testing"
)

// SetDBSourceHook wraps the database entry's reader during a restore,
// so a test can make the write fail part-way. Reset on cleanup.
func SetDBSourceHook(t testing.TB, hook func(io.Reader) io.Reader) {
	t.Helper()
	prev := dbSourceHook
	dbSourceHook = hook
	t.Cleanup(func() { dbSourceHook = prev })
}
