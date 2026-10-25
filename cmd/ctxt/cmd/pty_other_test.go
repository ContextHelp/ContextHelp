//go:build !darwin && !linux

package cmd

import (
	"errors"
	"os"
)

// openPTY is unavailable here; callers skip.
func openPTY() (master, slave *os.File, err error) {
	return nil, nil, errors.New("no pseudo-terminal support on this platform")
}
