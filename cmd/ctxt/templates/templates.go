// Package templates embeds the service-manager unit templates the ctxt
// binary renders, so the binary is self-contained.
//
// CapturePlist feeds `ctxt capture schedule install`: one LaunchAgent
// per browser profile and capture kind. It is a Go text/template body;
// see cmd/ctxt/cmd/capture_schedule_agent.go for the data shape and the
// xml escaping function every interpolated value goes through.
package templates

import _ "embed"

//go:embed com.contexthelp.ctxt.capture.plist
var CapturePlist string
