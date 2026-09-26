package chromium

import "fmt"

// Page transition bits stored in visits.transition, from Chromium's
// ui/base/page_transition_types.h:
// https://source.chromium.org/chromium/chromium/src/+/main:ui/base/page_transition_types.h
//
// The low byte is the core type (mutually exclusive values); the high bits
// are qualifier flags. Chromium stores the value as a signed 32-bit
// integer, so SERVER_REDIRECT (bit 31) reads back negative from SQLite;
// the bit tests below only look at the low 32 bits and work either way.
const (
	transitionCoreMask uint32 = 0xFF

	// Core types.
	transitionLink  uint32 = 0
	transitionTyped uint32 = 1
	// transitionAutoSubframe is content loaded into a subframe
	// automatically (ads, embeds). Not a user navigation.
	transitionAutoSubframe uint32 = 3
	// transitionManualSubframe is a user navigation inside a subframe.
	// Recorded so back/forward works; the top-level page is what the user
	// is on, so it is not a visit of its own here.
	transitionManualSubframe uint32 = 4
	transitionFormSubmit     uint32 = 7

	// Qualifiers.
	transitionChainStart uint32 = 0x10000000
	// transitionChainEnd marks the last visit of a redirect chain. A visit
	// that was not redirected is a chain of one, with both bits set.
	transitionChainEnd       uint32 = 0x20000000
	transitionClientRedirect uint32 = 0x40000000
	transitionServerRedirect uint32 = 0x80000000
)

// keptVisitSQL is the WHERE clause (on alias v) selecting one visit per
// user navigation:
//
//   - subframe loads (auto and manual) are dropped;
//   - of a redirect chain only the end is kept: the chain start and every
//     intermediate redirect carry no CHAIN_END bit.
var keptVisitSQL = fmt.Sprintf(
	"(v.transition & %d) NOT IN (%d, %d) AND (v.transition & %d) != 0",
	transitionCoreMask, transitionAutoSubframe, transitionManualSubframe,
	transitionChainEnd,
)
