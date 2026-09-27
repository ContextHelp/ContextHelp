// Package launch owns every browser process ctxt starts.
//
// Two kinds of launch live here:
//
//   - Open hands a URL to the user's default browser through the
//     platform handler: open on macOS, xdg-open on Linux and the BSDs,
//     rundll32 url.dll,FileProtocolHandler on Windows. Commands inject an
//     Opener so tests can record the URL instead.
//   - DumpDOM renders a page in headless Chrome or Chromium and returns
//     its DOM. Every headless run gets a fixed set of safety switches the
//     caller cannot drop or override (mockKeychain, a throwaway profile
//     removed afterwards, no sync, no extensions, ...), so a check never
//     touches the OS credential store or a real profile.
//
// FindChrome locates the headless browser: the CTXT_CHROME environment
// variable first (a path or command name, or off/0/false to disable),
// then the standard install locations, then PATH.
//
// Nothing else in the repository should exec a browser; a test in this
// package fails if headless switches appear outside it.
//
// This package is unrelated to package internal/browser, which manages
// the IBR daemon, and to internal/browser/chromium, which reads browser
// state from disk.
package launch
