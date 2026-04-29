// Package logger configures the global slog default to use kit/log's
// Charm-styled logger as its handler. Existing slog.Info/Warn/Error/Debug
// callers across the codebase pick up the styled output for free.
package logger

import (
	"io"
	"log/slog"
	"os"

	charmlog "charm.land/log/v2"
	"github.com/spf13/viper"
	kitlog "hop.top/kit/go/console/log"
)

// Init configures the default slog logger via kit/log. verbose=true sets
// level to Debug, otherwise InfoLevel (which kit/log auto-raises to Warn
// when viper key "quiet" is set, per kit/cli convention).
func Init(verbose bool) {
	InitWithWriter(verbose, os.Stderr)
}

// InitWithWriter configures the default slog logger with a custom writer.
// Intended for testing.
func InitWithWriter(verbose bool, w io.Writer) {
	count := 0
	if verbose {
		count = 1
	}
	l := kitlog.WithVerbose(viper.GetViper(), count)
	l.SetOutput(w)
	// At the slog API level, "verbose=false" historically meant Warn.
	// kit/log's default is InfoLevel; tests + downstream callers
	// expect Warn-level filtering when not verbose.
	if !verbose && !viper.GetBool("quiet") {
		l.SetLevel(charmlog.WarnLevel)
	}
	slog.SetDefault(slog.New(l))
}
