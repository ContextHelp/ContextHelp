package cmd

// LaunchAgent model behind `ctxt capture schedule`: label scheme,
// plist rendering and parsing, and the launchctl seam. The cobra
// surface lives in capture_schedule.go.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/template"
	"time"
	"unicode"

	"github.com/ideacrafterslabs/ctxt/cmd/ctxt/templates"
	"github.com/ideacrafterslabs/ctxt/internal/binpath"
)

// goosDarwin is the only GOOS capture schedules support (launchd).
const goosDarwin = "darwin"

// Plist boolean element names.
const (
	plistTrueTag  = "true"
	plistFalseTag = "false"
)

// scheduleKind names the capture subcommand an agent runs.
type scheduleKind string

const (
	kindHistory scheduleKind = "history"
	kindTabs    scheduleKind = "tabs"
)

// scheduleKinds lists every kind in install order.
var scheduleKinds = []scheduleKind{kindHistory, kindTabs}

const (
	// scheduleLabelPrefix starts every capture agent label; list and
	// uninstall only ever touch files carrying it.
	scheduleLabelPrefix = "com.contexthelp.ctxt.capture-"

	// scheduleLogPrefix is stripped from the label to name log files.
	scheduleLogPrefix = "com.contexthelp.ctxt."

	// maxSlugLen caps the readable part of the label; the hash keeps
	// truncated slugs distinct.
	maxSlugLen = 24
)

// scheduleLabel returns the launchd label for one (kind, browser,
// profile). The profile is reduced to a lowercase ASCII slug for
// readability and suffixed with a hash of the exact browser and
// profile so "Work", "work" and "Work!" never collide. The result is
// safe as a file name and as a launchctl service target.
func scheduleLabel(kind scheduleKind, browser, profile string) string {
	sum := sha256.Sum256([]byte(browser + "\x00" + profile))
	return fmt.Sprintf("%s%s.%s.%s-%s",
		scheduleLabelPrefix, kind, browser, profileSlug(profile), hex.EncodeToString(sum[:4]))
}

func profileSlug(profile string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(profile) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.TrimRight(b.String(), "-")
	if len(s) > maxSlugLen {
		s = strings.TrimRight(s[:maxSlugLen], "-")
	}
	if s == "" {
		return "profile"
	}
	return s
}

// validateBrowserProfile rejects values launchd cannot carry: empty
// names and control characters (XML 1.0 forbids most of them, and a
// newline in an argument is never a real profile name).
func validateBrowserProfile(p string) error {
	if strings.TrimSpace(p) == "" {
		return errors.New("--browser-profile is required")
	}
	for _, r := range p {
		if unicode.IsControl(r) {
			return fmt.Errorf("--browser-profile %q contains a control character", p)
		}
	}
	return nil
}

// scheduleAgent is one LaunchAgent: a capture kind run on an interval
// for one browser profile.
type scheduleAgent struct {
	Label          string
	Kind           scheduleKind
	Browser        string
	BrowserProfile string
	Instance       string
	FocusProfile   string
	Binary         string
	LogDir         string
	Home           string
	PathEnv        string
	Interval       time.Duration
}

func (a scheduleAgent) label() string {
	if a.Label != "" {
		return a.Label
	}
	return scheduleLabel(a.Kind, a.Browser, a.BrowserProfile)
}

func (a scheduleAgent) logBase() string {
	return filepath.Join(a.LogDir, strings.TrimPrefix(a.label(), scheduleLogPrefix))
}

// StdoutPath and StderrPath follow the dpkms layout:
// ~/Library/Logs/<binary>/<name>.{out,err}.log.
func (a scheduleAgent) StdoutPath() string { return a.logBase() + ".out.log" }
func (a scheduleAgent) StderrPath() string { return a.logBase() + ".err.log" }

// ProgramArguments is the argv launchd execs directly — no shell, so
// the profile name travels as one argument whatever it contains.
func (a scheduleAgent) ProgramArguments() []string {
	args := []string{a.Binary, "capture", string(a.Kind), "--browser", a.Browser, "--browser-profile", a.BrowserProfile}
	if a.Instance != "" {
		args = append(args, "--instance", a.Instance)
	}
	if a.FocusProfile != "" {
		args = append(args, "--profile", a.FocusProfile)
	}
	return args
}

type schedulePlistData struct {
	Label            string
	ProgramArguments []string
	StartInterval    int64
	StdoutPath       string
	StderrPath       string
	WorkingDirectory string
	HomeDir          string
	PathEnv          string
}

var schedulePlistTmpl = template.Must(template.New("capture-plist").
	Funcs(template.FuncMap{"xml": xmlEscape}).
	Parse(templates.CapturePlist))

func xmlEscape(s string) (string, error) {
	var b strings.Builder
	if err := xml.EscapeText(&b, []byte(s)); err != nil {
		return "", err
	}
	return b.String(), nil
}

// renderSchedulePlist renders a's LaunchAgent plist.
func renderSchedulePlist(a scheduleAgent) (string, error) {
	var buf bytes.Buffer
	err := schedulePlistTmpl.Execute(&buf, schedulePlistData{
		Label:            a.label(),
		ProgramArguments: a.ProgramArguments(),
		StartInterval:    int64(a.Interval / time.Second),
		StdoutPath:       a.StdoutPath(),
		StderrPath:       a.StderrPath(),
		WorkingDirectory: a.Home,
		HomeDir:          a.Home,
		PathEnv:          a.PathEnv,
	})
	if err != nil {
		return "", fmt.Errorf("render capture plist: %w", err)
	}
	return buf.String(), nil
}

// parseSchedulePlist reads back a plist this package rendered. It
// recovers the agent from ProgramArguments rather than trusting the
// label, so list reports what launchd will actually run.
func parseSchedulePlist(data []byte) (scheduleAgent, error) {
	top, err := decodePlistDict(data)
	if err != nil {
		return scheduleAgent{}, err
	}
	var a scheduleAgent
	a.Label, _ = top["Label"].(string)
	if n, ok := top["StartInterval"].(int64); ok {
		a.Interval = time.Duration(n) * time.Second
	}
	raw, _ := top["ProgramArguments"].([]any)
	args := make([]string, 0, len(raw))
	for _, v := range raw {
		s, _ := v.(string)
		args = append(args, s)
	}
	if len(args) < 3 || args[1] != "capture" {
		return scheduleAgent{}, fmt.Errorf("plist %q does not run ctxt capture", a.Label)
	}
	a.Binary = args[0]
	a.Kind = scheduleKind(args[2])
	for i := 3; i+1 < len(args); i += 2 {
		switch args[i] {
		case "--browser":
			a.Browser = args[i+1]
		case "--browser-profile":
			a.BrowserProfile = args[i+1]
		case "--instance":
			a.Instance = args[i+1]
		case "--profile":
			a.FocusProfile = args[i+1]
		}
	}
	return a, nil
}

// decodePlistDict decodes the top-level <dict> of an XML plist into
// Go values: string, int64, bool, []any and map[string]any.
func decodePlistDict(data []byte) (map[string]any, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("decode plist: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "dict" {
			v, err := decodePlistValue(dec, se)
			if err != nil {
				return nil, fmt.Errorf("decode plist: %w", err)
			}
			m, _ := v.(map[string]any)
			return m, nil
		}
	}
}

func decodePlistValue(dec *xml.Decoder, se xml.StartElement) (any, error) {
	switch se.Name.Local {
	case "string", "integer", "real", "date", "data":
		var s string
		if err := dec.DecodeElement(&s, &se); err != nil {
			return nil, err
		}
		if se.Name.Local == "integer" {
			return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		}
		return s, nil
	case plistTrueTag, plistFalseTag:
		if err := dec.Skip(); err != nil {
			return nil, err
		}
		return se.Name.Local == plistTrueTag, nil
	case "array":
		var out []any
		for {
			child, end, err := nextPlistElement(dec)
			if err != nil || end {
				return out, err
			}
			v, err := decodePlistValue(dec, child)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
	case "dict":
		out := map[string]any{}
		for {
			keyEl, end, err := nextPlistElement(dec)
			if err != nil || end {
				return out, err
			}
			var key string
			if err := dec.DecodeElement(&key, &keyEl); err != nil {
				return nil, err
			}
			valEl, end, err := nextPlistElement(dec)
			if err != nil {
				return nil, err
			}
			if end {
				return nil, fmt.Errorf("key %q has no value", key)
			}
			v, err := decodePlistValue(dec, valEl)
			if err != nil {
				return nil, err
			}
			out[key] = v
		}
	default:
		return nil, dec.Skip()
	}
}

// nextPlistElement returns the next child start element, or end=true
// when the enclosing element closes.
func nextPlistElement(dec *xml.Decoder) (xml.StartElement, bool, error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return xml.StartElement{}, false, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			return t, false, nil
		case xml.EndElement:
			return xml.StartElement{}, true, nil
		}
	}
}

// launchctl is the seam between the commands and the host service
// manager. Tests substitute a fake; nothing in the test suite reaches
// the real launchctl.
type launchctl interface {
	// Bootstrap loads the agent at plistPath into the user's GUI domain.
	Bootstrap(plistPath string) error
	// Bootout unloads the agent. Best-effort: an agent that is not
	// loaded is already in the desired state.
	Bootout(label, plistPath string)
	// Loaded reports whether launchd currently knows the label.
	Loaded(label string) bool
}

// execLaunchctl drives the launchctl on PATH, mirroring dpkms install:
// modern bootstrap/bootout first, legacy load/unload as fallback.
type execLaunchctl struct{}

func (execLaunchctl) domain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

func (l execLaunchctl) Bootstrap(plistPath string) error {
	bin, err := exec.LookPath("launchctl")
	if err != nil {
		return fmt.Errorf("launchctl not found on PATH: %w", err)
	}
	// #nosec G204 -- bin is launchctl from PATH; domain and plistPath are process-local.
	if err := exec.Command(bin, "bootstrap", l.domain(), plistPath).Run(); err == nil {
		return nil
	}
	// #nosec G204 -- see above.
	if err := exec.Command(bin, "load", plistPath).Run(); err != nil {
		return fmt.Errorf("launchctl load %s: %w", plistPath, err)
	}
	return nil
}

func (l execLaunchctl) Bootout(label, plistPath string) {
	bin, err := exec.LookPath("launchctl")
	if err != nil {
		return
	}
	// #nosec G204 -- bin is launchctl from PATH; label is generated by scheduleLabel.
	if err := exec.Command(bin, "bootout", l.domain()+"/"+label).Run(); err == nil {
		return
	}
	// #nosec G204 -- see above.
	_ = exec.Command(bin, "unload", plistPath).Run()
}

func (l execLaunchctl) Loaded(label string) bool {
	bin, err := exec.LookPath("launchctl")
	if err != nil {
		return false
	}
	// #nosec G204 -- bin is launchctl from PATH; label comes from a capture plist.
	return exec.Command(bin, "print", l.domain()+"/"+label).Run() == nil
}

// scheduleEnv carries everything host-specific the commands touch, so
// tests can point them at temp dirs and a fake launchctl.
type scheduleEnv struct {
	launchctl launchctl
	goos      string
	home      string
	agentsDir string
	logDir    string
	binary    string
	pathEnv   string
}

// newScheduleEnv resolves the real host environment. Replaced in tests.
var newScheduleEnv = defaultScheduleEnv

// scheduleExecutable is os.Executable; tests replace it.
var scheduleExecutable = os.Executable

func defaultScheduleEnv() (*scheduleEnv, error) {
	// Same binary resolution as dpkms install: the ctxt that ran
	// install is the one launchd will exec, recorded through the
	// Homebrew prefix shim when there is one so `brew upgrade` does
	// not strand the agents on a removed keg.
	exe, err := scheduleExecutable()
	if err != nil {
		return nil, fmt.Errorf("resolve current binary: %w", err)
	}
	bin := binpath.Stable(exe)
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		pathEnv = "/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
	}
	return &scheduleEnv{
		launchctl: execLaunchctl{},
		goos:      runtime.GOOS,
		home:      home,
		agentsDir: filepath.Join(home, "Library", "LaunchAgents"),
		logDir:    filepath.Join(home, "Library", "Logs", "ctxt"),
		binary:    bin,
		pathEnv:   pathEnv,
	}, nil
}

func (e *scheduleEnv) plistPath(label string) string {
	return filepath.Join(e.agentsDir, label+".plist")
}
