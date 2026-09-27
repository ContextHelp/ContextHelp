package cmd

import "github.com/ideacrafterslabs/ctxt/internal/cli/banner"

// upgradeBannerTTY reports whether the banner's writer is a terminal.
// Tests replace it.
var upgradeBannerTTY = banner.IsTerminal
