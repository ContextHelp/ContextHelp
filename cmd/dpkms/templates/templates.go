// Package templates embeds the dpkms install service-manager unit
// templates so the binary is self-contained.
//
// The plist template feeds darwin LaunchAgent installs; the systemd
// template feeds linux user-service installs. Both are Go text/template
// bodies — see cmd/dpkms/cmd/install_launchd.go for the data shapes.
package templates

import _ "embed"

//go:embed com.contexthelp.dpkms.plist
var Plist string

//go:embed dpkms.service
var Systemd string
