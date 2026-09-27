package launch

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Opener opens a URL in the user's browser.
type Opener interface {
	Open(ctx context.Context, url string) error
}

// System is the Opener that uses the platform's default browser handler.
type System struct{}

// Open opens url in the user's default browser.
func Open(ctx context.Context, url string) error {
	return System{}.Open(ctx, url)
}

// OpenCommand returns the command that opens url in the default browser
// on goos, and false on a platform with no known handler.
func OpenCommand(goos, url string) (name string, args []string, ok bool) {
	switch goos {
	case goosDarwin:
		return "open", []string{url}, true
	case goosWindows:
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}, true
	case goosLinux, "freebsd", "openbsd", "netbsd", "dragonfly", "solaris", "illumos":
		return "xdg-open", []string{url}, true
	}
	return "", nil, false
}

// Open starts the platform handler and does not wait for it: some
// handlers stay up as long as the browser does. The handler outlives a
// cancelled ctx on purpose, since killing it could take the browser
// window down with it; it is reaped in the background.
func (System) Open(ctx context.Context, url string) error {
	if err := checkURL(url); err != nil {
		return err
	}
	name, args, ok := OpenCommand(runtime.GOOS, url)
	if !ok {
		return fmt.Errorf("no default browser handler known for %s", runtime.GOOS)
	}
	c := exec.CommandContext(context.WithoutCancel(ctx), name, args...) //nolint:gosec // fixed handler; url cannot be read as a switch
	if err := c.Start(); err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}
	go func() { _ = c.Wait() }()
	return nil
}

// checkURL refuses a target a browser or handler would read as a switch.
func checkURL(url string) error {
	switch {
	case url == "":
		return errors.New("launch: empty url")
	case strings.HasPrefix(url, "-"):
		return fmt.Errorf("launch: url %q starts with '-'", url)
	}
	return nil
}
