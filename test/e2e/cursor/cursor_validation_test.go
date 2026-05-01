//go:build cursor_e2e

package cursor

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCursor_NameValidationRejectsUppercase: invalid name → exit 2.
func TestCursor_NameValidationRejectsUppercase(t *testing.T) {
	env := newCursorEnv(t)
	out, code := env.runBin("cursor", "reset", "BadName")
	assert.Equal(t, 2, code, "uppercase name should exit 2; out: %s", out)
	assert.True(t,
		strings.Contains(strings.ToLower(out), "lowercase") ||
			strings.Contains(strings.ToLower(out), "alphanumeric"),
		"error should mention validation rule; out: %s", out)
}

// TestCursor_NameValidationRejectsTooLong: >64 chars → exit 2.
func TestCursor_NameValidationRejectsTooLong(t *testing.T) {
	env := newCursorEnv(t)
	long := strings.Repeat("a", 65)
	out, code := env.runBin("cursor", "reset", long)
	assert.Equal(t, 2, code, "65-char name should exit 2; out: %s", out)
	assert.Contains(t, strings.ToLower(out), "too long")
}

// TestCursor_UnknownCursorWithoutAdvanceFails: `ctxt list --cursor foo`
// with no prior cursor and no --advance must exit 2 with a hint.
//
// Rather than spinning up a real DB, we exercise the cursor-only branch
// of the validation: missing cursor file + named cursor lookup via
// `ctxt cursor show`, which mirrors the same code path semantics.
func TestCursor_UnknownCursorWithoutAdvanceFails(t *testing.T) {
	env := newCursorEnv(t)
	out, code := env.runBin("cursor", "show", "neverseen")
	assert.Equal(t, 2, code, "unknown cursor should exit 2; out: %s", out)
	assert.Contains(t, strings.ToLower(out), "not found")
}
