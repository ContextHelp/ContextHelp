package banner

import (
	"fmt"
	"io"
	"sync"

	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// Once prints the upgrade banner at most once: for the first status it is
// shown that renders a line (see Format). A command that makes many API
// calls during an upgrade reports it once, not per response. Safe for
// concurrent use.
type Once struct {
	w    io.Writer
	once sync.Once
}

// NewOnce returns a Once that writes to w.
func NewOnce(w io.Writer) *Once { return &Once{w: w} }

// Show prints st's banner unless one was already printed. A status that
// renders nothing (idle) does not use up the one line.
func (o *Once) Show(st upgrade.Status) {
	line := Format(st)
	if line == "" {
		return
	}
	o.once.Do(func() { _, _ = fmt.Fprintln(o.w, line) })
}
