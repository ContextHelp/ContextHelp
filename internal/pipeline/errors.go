package pipeline

import "errors"

// PermanentError wraps an error that should not be retried.
type PermanentError struct {
	Err error
}

func (e *PermanentError) Error() string {
	if e.Err == nil {
		return "<nil>"
	}
	return e.Err.Error()
}
func (e *PermanentError) Unwrap() error { return e.Err }

// Permanent wraps err as a PermanentError.
// Returns nil when err is nil; avoids double-wrapping if err is already permanent.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	var pe *PermanentError
	if errors.As(err, &pe) {
		return err
	}
	return &PermanentError{Err: err}
}

// IsPermanent reports whether err (or any error in its chain) is permanent.
func IsPermanent(err error) bool {
	var pe *PermanentError
	return errors.As(err, &pe)
}
