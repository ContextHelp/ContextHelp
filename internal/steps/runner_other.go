//go:build !unix

package steps

import (
	"context"
	"fmt"
)

// ProcessRunner is unavailable off unix: steps there need container
// isolation.
type ProcessRunner struct{}

// Run always fails: process isolation needs a unix host.
func (ProcessRunner) Run(context.Context, Invocation) (Result, error) {
	return Result{ExitCode: -1}, fmt.Errorf("%w: process isolation needs a unix host; set isolation: container", ErrIsolationUnavailable)
}
