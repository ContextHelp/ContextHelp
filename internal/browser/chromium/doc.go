// Package chromium reads on-disk state of Chromium-family browsers
// (Chrome, Brave, Edge, Arc, Chromium, Vivaldi) without talking to a
// running browser.
//
// Profile resolution maps a user-facing profile name ("Work") or a profile
// folder name ("Profile 1") to the profile directory inside the browser's
// user data directory, using the "Local State" JSON file Chromium keeps at
// the root of that directory. Readers of per-profile files (sessions,
// history) take the resolved Profile.Dir.
//
// The user data directory defaults to the vendor location for the current
// OS. It can be overridden per call with WithUserDataDir, or per browser
// with the CTXT_<BROWSER>_USER_DATA_DIR environment variable (for example
// CTXT_BRAVE_USER_DATA_DIR) for portable or non-default installs.
//
// This package is unrelated to package internal/browser, which manages the
// IBR daemon.
package chromium
