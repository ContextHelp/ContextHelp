// Command headless-chrome runs a page through headless Chrome with the
// same launch rules the e2e suite uses (package internal/browser/launch):
// mock keychain, a throwaway profile removed afterwards, no sync, no
// extensions. It is a developer tool for manual page checks, run through
// scripts/headless-chrome.sh; it is not shipped.
//
// Usage:
//
//	headless-chrome dump [flags] <url-or-file> [-- chrome-switch...]
//	headless-chrome path
//
// dump prints the rendered DOM on stdout. A target without a URL scheme
// is a local file. path prints the Chrome that would run; set CTXT_CHROME
// to choose another.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/browser/launch"
)

const usage = `usage:
  headless-chrome dump [flags] <url-or-file> [-- chrome-switch...]
  headless-chrome path

dump prints the DOM of the page after headless Chrome renders it; a
target without a URL scheme is a local file. path prints the Chrome that
would run. Set ` + launch.EnvChrome + ` to a Chrome or Chromium to use another.

dump flags:
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run is main without the process: it returns the exit code, 2 for a
// usage error.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "path":
		if len(args) != 1 {
			fmt.Fprint(stderr, usage)
			return 2
		}
		p, err := launch.FindChrome()
		if err != nil {
			fmt.Fprintln(stderr, "headless-chrome:", err)
			return 1
		}
		fmt.Fprintln(stdout, p)
		return 0
	case "dump":
		return dump(ctx, args[1:], stdout, stderr)
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	}
	fmt.Fprintf(stderr, "headless-chrome: unknown command %q\n", args[0])
	fmt.Fprint(stderr, usage)
	return 2
}

func dump(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("dump", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage); fs.PrintDefaults() }
	timeout := fs.Duration("timeout", launch.DefaultTimeout, "bound on one render attempt")
	attempts := fs.Int("attempts", 1, "renders to try before giving up")
	until := fs.String("until", "", "retry until the DOM matches this regular expression")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fs.Usage()
		return 2
	}
	var extra []string
	if len(rest) > 1 {
		if rest[1] != "--" {
			fmt.Fprintf(stderr, "headless-chrome: unexpected %q; put Chrome switches after --\n", rest[1])
			return 2
		}
		extra = rest[2:]
	}
	target, err := targetURL(rest[0])
	if err != nil {
		fmt.Fprintln(stderr, "headless-chrome:", err)
		return 2
	}
	opts := launch.Options{Args: extra, Timeout: *timeout, Attempts: *attempts}
	if *until != "" {
		re, err := regexp.Compile(*until)
		if err != nil {
			fmt.Fprintln(stderr, "headless-chrome: --until:", err)
			return 2
		}
		opts.Accept = re.MatchString
	}
	dom, err := launch.DumpDOM(ctx, target, opts)
	if dom != "" {
		fmt.Fprint(stdout, dom)
	}
	if err != nil {
		fmt.Fprintln(stderr, "headless-chrome:", err)
		return 1
	}
	return 0
}

// targetURL returns arg as a URL: unchanged when it has a scheme,
// otherwise as a file:// URL for the local file it names.
func targetURL(arg string) (string, error) {
	if u, err := url.Parse(arg); err == nil && len(u.Scheme) > 1 {
		return arg, nil
	}
	abs, err := filepath.Abs(arg)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", err
	}
	p := filepath.ToSlash(abs)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // a Windows drive path: file:///C:/...
	}
	return (&url.URL{Scheme: "file", Path: p}).String(), nil
}
