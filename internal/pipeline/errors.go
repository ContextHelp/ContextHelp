package pipeline

import "fmt"

// PermanentError wraps an error that should not be retried.
// Use Permanent() to construct one and errors.As() to detect it.
type PermanentError struct {
	Err error
}

func (e *PermanentError) Error() string {
	return fmt.Sprintf("permanent: %v", e.Err)
}

func (e *PermanentError) Unwrap() error {
	return e.Err
}

// Permanent wraps err as a PermanentError, signalling that retries are futile.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Err: err}
}
