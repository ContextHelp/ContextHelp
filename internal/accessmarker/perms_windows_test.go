//go:build windows

package accessmarker

import (
	"errors"
	"testing"
)

// Windows access is an ACL: the mode check says it cannot answer rather
// than reporting a false all-clear.
func TestCheckPermissions_Unsupported(t *testing.T) {
	if _, err := CheckPermissions(`C:\db.sqlite`, ""); !errors.Is(err, ErrModesUnsupported) {
		t.Fatalf("CheckPermissions on windows = %v; want ErrModesUnsupported", err)
	}
}
