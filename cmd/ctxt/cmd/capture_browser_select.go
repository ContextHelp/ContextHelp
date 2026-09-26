package cmd

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/ideacrafterslabs/ctxt/internal/browser/chromium"
	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"
)

// captureBrowserSelector builds the selector the capture commands use.
// Tests replace it to point at synthetic installs and a fixed OS
// default browser.
var captureBrowserSelector = func() chromium.Selector { return chromium.Selector{} }

// configuredCaptureBrowser returns capture.browser, "" when unset.
func configuredCaptureBrowser() string {
	if cfg == nil {
		return ""
	}
	return cfg.Capture.Browser
}

// resolveCaptureTarget turns --browser / --browser-profile (either may
// be empty) into one profile on disk, via chromium.Selector.Select:
// the flag, then capture.browser, then the one installed browser
// holding the profile, then the OS default browser. An omitted profile
// is the browser's last-used one.
//
// Anything picked automatically is named on stderr. A name that
// matches nothing, or several things, is a usage error (exit 2) listing
// the candidates; browser state missing from disk is NOT_FOUND (exit 3).
func resolveCaptureTarget(cmd *cobra.Command, browserFlag, profileFlag string) (chromium.Profile, error) {
	req := chromium.Request{Browser: browserFlag, ConfigBrowser: configuredCaptureBrowser(), Profile: profileFlag}
	sel, err := captureBrowserSelector().Select(req)
	if err != nil {
		return chromium.Profile{}, captureTargetError(err)
	}
	if note := selectionNote(sel); note != "" {
		fmt.Fprintln(cmd.ErrOrStderr(), note)
	}
	return sel.Profile, nil
}

// selectionNote says what was picked automatically, "" when the caller
// named everything (flag or config).
func selectionNote(sel chromium.Selection) string {
	var browserWhy, profileWhy string
	switch sel.BrowserSource {
	case chromium.SourceOSDefault:
		browserWhy = "the OS default browser"
	case chromium.SourceProfileMatch:
		browserWhy = "the only browser with that profile"
	}
	switch sel.ProfileSource {
	case chromium.SourceLastUsed:
		profileWhy = "its last-used profile"
	case chromium.SourceOnlyProfile:
		profileWhy = "its only profile"
	}
	if browserWhy == "" && profileWhy == "" {
		return ""
	}
	why := browserWhy
	switch {
	case why == "":
		why = profileWhy
	case profileWhy != "":
		why += ", " + profileWhy
	}
	p := sel.Profile
	return fmt.Sprintf("using %s profile %q (%s): %s", p.Browser, p.Name, p.DirName, why)
}

// captureTargetError maps a selection failure to the CLI's error kinds.
func captureTargetError(err error) error {
	const pick = "pass --browser (one of: %s) or set capture.browser"
	var notInstalled *chromium.NotInstalledError
	switch {
	case errors.Is(err, chromium.ErrNoBrowser):
		e := output.UsageError(err.Error())
		e.SuggestedFix = fmt.Sprintf(pick, browserList())
		return e
	case errors.Is(err, chromium.ErrNoProfile):
		e := output.UsageError(err.Error())
		e.SuggestedFix = `pass --browser-profile (a profile name such as "Work", or a folder name such as "Profile 1")`
		return e
	case errors.Is(err, chromium.ErrUnknownBrowser),
		errors.Is(err, chromium.ErrProfileNotFound),
		errors.Is(err, chromium.ErrAmbiguousProfile),
		errors.Is(err, chromium.ErrUnsupported):
		return output.UsageError(err.Error())
	case errors.Is(err, chromium.ErrProfileDirMissing):
		return output.NotFoundError(err.Error())
	case errors.As(err, &notInstalled):
		e := output.NotFoundError(err.Error())
		e.SuggestedFix = fmt.Sprintf("set %s to the browser's user data directory", chromium.EnvUserDataDir(notInstalled.Browser))
		return e
	case errors.Is(err, fs.ErrNotExist):
		return output.NotFoundError(err.Error())
	default:
		return err
	}
}
