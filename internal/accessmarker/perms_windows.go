//go:build windows

package accessmarker

// CheckPermissions is not supported on Windows: access there is an ACL,
// which this package does not inspect. It returns ErrModesUnsupported.
func CheckPermissions(_, _ string) ([]Finding, error) {
	return nil, ErrModesUnsupported
}

// OwnedByCurrentUser reports true on Windows: file ownership there is an
// ACL question this package does not answer, so a maintenance override
// relies on the operating system refusing the open instead.
func OwnedByCurrentUser(_ string) (bool, int, error) {
	return true, -1, nil
}
