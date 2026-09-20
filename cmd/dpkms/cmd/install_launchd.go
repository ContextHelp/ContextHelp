// Package cmd: dpkms install — registers the dpkms binary as a managed
// service so the daemon auto-starts on login and auto-restarts on crash.
//
// Platforms:
//
//   - darwin: writes a LaunchAgent plist to ~/Library/LaunchAgents/ and
//     loads it via launchctl bootstrap (modern) with a launchctl load
//     fallback for older macOS.
//   - linux: writes a systemd user unit to ~/.config/systemd/user/ and
//     enables it via systemctl --user.
//
// The command is intentionally additive: it does not touch stored
// knowledge or the pipeline contract, only registers the running binary
// with the host service manager.
package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/template"

	"github.com/ideacrafterslabs/ctxt/cmd/dpkms/templates"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/spf13/cobra"
)

const (
	// LaunchdLabel is the canonical LaunchAgent / systemd-unit identifier.
	LaunchdLabel = "com.contexthelp.dpkms"

	// SystemdUnitName is the canonical systemd-user-service file name.
	SystemdUnitName = "dpkms.service"

	// defaultThrottleInterval is the launchd ThrottleInterval (seconds)
	// — the minimum time between restarts; protects against tight
	// crash loops.
	defaultThrottleInterval = 30

	// defaultRestartSec is the systemd RestartSec (seconds).
	defaultRestartSec = 30
)

// PlistData fills cmd/dpkms/templates/com.contexthelp.dpkms.plist.
type PlistData struct {
	Label            string
	BinaryPath       string
	ThrottleInterval int
	StdoutPath       string
	StderrPath       string
	WorkingDirectory string
	HomeDir          string
	PathEnv          string
}

// SystemdData fills cmd/dpkms/templates/dpkms.service.
type SystemdData struct {
	BinaryPath       string
	RestartSec       int
	StdoutPath       string
	StderrPath       string
	WorkingDirectory string
	HomeDir          string
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Register dpkms with the host service manager (launchd / systemd)",
	Long: `Install the running dpkms binary as a managed service.

The platform is auto-detected:

  - darwin: a LaunchAgent plist is rendered to
    ~/Library/LaunchAgents/com.contexthelp.dpkms.plist and loaded with
    launchctl. The agent runs at login (RunAtLoad) and is restarted on
    crash (KeepAlive) with a ThrottleInterval to prevent tight loops.

  - linux: a systemd user unit is rendered to
    ~/.config/systemd/user/dpkms.service and enabled with
    "systemctl --user enable --now". Restart=always with RestartSec=30.

The binary path is resolved via os.Executable() — whichever dpkms binary
ran "dpkms install" is the one the service manager will launch.

Logs land at:

  - darwin: ~/Library/Logs/dpkms/{out,err}.log
  - linux:  ~/.local/share/dpkms/dpkms.{out,err}.log

The command is idempotent: re-running it on an already-installed host
prints the current status and exits 0. Pass --force to overwrite the
service file and reload.

Examples:
  dpkms install --launchd         # darwin (or --systemd on linux)
  dpkms install --status          # show install + running state
  dpkms install --uninstall       # stop service + remove unit file
  dpkms install --launchd --force # re-render and reload`,
	RunE: runInstall,
}

func init() {
	rootCmd.AddCommand(installCmd)
	installCmd.Flags().Bool("launchd", false, "install as a macOS LaunchAgent (darwin only)")
	installCmd.Flags().Bool("systemd", false, "install as a systemd user service (linux only)")
	installCmd.Flags().Bool("uninstall", false, "stop the service and remove the unit file")
	installCmd.Flags().Bool("status", false, "print current install + running state and exit")
	installCmd.Flags().Bool("force", false, "overwrite an existing unit file and reload the service manager")

	// Renders a unit file and drives launchctl/systemctl. --uninstall
	// stops the service and removes the unit; --force overwrites an
	// existing one. Both lose prior service state. Destructive.
	cliconv.WithSideEffect(installCmd, cliconv.SideEffectDestructive)
}

// installer is the per-platform contract install/uninstall/status share.
type installer interface {
	UnitPath() (string, error)
	IsInstalled() (bool, error)
	IsRunning() (bool, error)
	Install(force bool) error
	Uninstall() error
}

func runInstall(cmd *cobra.Command, _ []string) error {
	uninstall, _ := cmd.Flags().GetBool("uninstall")
	status, _ := cmd.Flags().GetBool("status")
	force, _ := cmd.Flags().GetBool("force")
	// --launchd / --systemd are accepted but optional: the platform is
	// auto-detected. The flags exist so the surface matches the spec
	// and future help-text can suggest the explicit form.
	_, _ = cmd.Flags().GetBool("launchd")
	_, _ = cmd.Flags().GetBool("systemd")

	inst, err := newInstaller()
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()

	switch {
	case status:
		return printStatus(w, inst)
	case uninstall:
		return inst.Uninstall()
	default:
		installed, err := inst.IsInstalled()
		if err != nil {
			return err
		}
		if installed && !force {
			fmt.Fprintln(w, formatAlreadyInstalled(inst))
			return nil
		}
		if err := inst.Install(force); err != nil {
			return err
		}
		fmt.Fprintln(w, "dpkms registered with the host service manager.")
		return printStatus(w, inst)
	}
}

func newInstaller() (installer, error) {
	bin, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve current binary: %w", err)
	}
	bin, err = filepath.EvalSymlinks(bin)
	if err != nil {
		// EvalSymlinks fails when the binary path doesn't exist on disk
		// (e.g. the test harness). Fall back to the raw path.
		bin, _ = os.Executable()
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}

	switch runtime.GOOS {
	case "darwin":
		return &darwinInstaller{binary: bin, home: home}, nil
	case "linux":
		return &linuxInstaller{binary: bin, home: home}, nil
	default:
		return nil, fmt.Errorf("unsupported platform: %s (only darwin and linux are supported)", runtime.GOOS)
	}
}

func formatAlreadyInstalled(inst installer) string {
	state := "stopped"
	if running, _ := inst.IsRunning(); running {
		state = "running"
	}
	switch inst.(type) {
	case *darwinInstaller:
		return fmt.Sprintf("dpkms is already installed as a LaunchAgent; status: %s. Pass --force to reinstall.", state)
	case *linuxInstaller:
		return fmt.Sprintf("dpkms is already installed as a systemd user service; status: %s. Pass --force to reinstall.", state)
	default:
		return fmt.Sprintf("dpkms is already installed; status: %s.", state)
	}
}

func printStatus(w io.Writer, inst installer) error {
	path, _ := inst.UnitPath()
	installed, err := inst.IsInstalled()
	if err != nil {
		return err
	}
	running, _ := inst.IsRunning()
	fmt.Fprintf(w, "unit:      %s\n", path)
	fmt.Fprintf(w, "installed: %t\n", installed)
	fmt.Fprintf(w, "running:   %t\n", running)
	return nil
}

// ─── darwin ──────────────────────────────────────────────────────────

type darwinInstaller struct {
	binary string
	home   string
}

func (d *darwinInstaller) UnitPath() (string, error) {
	return filepath.Join(d.home, "Library", "LaunchAgents", LaunchdLabel+".plist"), nil
}

func (d *darwinInstaller) logDir() string {
	return filepath.Join(d.home, "Library", "Logs", "dpkms")
}

func (d *darwinInstaller) IsInstalled() (bool, error) {
	p, _ := d.UnitPath()
	_, err := os.Stat(p)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (d *darwinInstaller) IsRunning() (bool, error) {
	if _, err := exec.LookPath("launchctl"); err != nil {
		return false, nil
	}
	out, err := exec.Command("launchctl", "list").Output()
	if err != nil {
		return false, nil
	}
	return strings.Contains(string(out), LaunchdLabel), nil
}

func (d *darwinInstaller) Install(force bool) error {
	if err := os.MkdirAll(d.logDir(), 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	plistPath, _ := d.UnitPath()
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}

	rendered, err := RenderPlist(PlistData{
		Label:            LaunchdLabel,
		BinaryPath:       d.binary,
		ThrottleInterval: defaultThrottleInterval,
		StdoutPath:       filepath.Join(d.logDir(), "dpkms.out.log"),
		StderrPath:       filepath.Join(d.logDir(), "dpkms.err.log"),
		WorkingDirectory: d.home,
		HomeDir:          d.home,
		PathEnv:          defaultPathEnv(),
	})
	if err != nil {
		return err
	}

	if force {
		// best-effort unload before overwrite — the agent must not be
		// holding the binary open when we re-render.
		_ = d.bootout(plistPath)
	}

	if err := os.WriteFile(plistPath, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	return d.bootstrap(plistPath)
}

func (d *darwinInstaller) Uninstall() error {
	plistPath, _ := d.UnitPath()
	_ = d.bootout(plistPath)
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}
	return nil
}

// bootstrap loads the agent. Modern macOS prefers
// `launchctl bootstrap gui/<uid> <plist>`; older releases accept
// `launchctl load <plist>`. We try the modern form first and fall
// back if it's rejected.
func (d *darwinInstaller) bootstrap(plistPath string) error {
	if _, err := exec.LookPath("launchctl"); err != nil {
		return fmt.Errorf("launchctl not found on PATH: %w", err)
	}
	uid := strconv.Itoa(os.Getuid())
	// #nosec G204 -- binary is the literal "launchctl"; uid comes from
	// os.Getuid and plistPath from UnitPath, both process-local.
	if err := exec.Command("launchctl", "bootstrap", "gui/"+uid, plistPath).Run(); err == nil {
		return nil
	}
	// fall back to deprecated load
	// #nosec G204 -- literal binary; plistPath is process-local.
	if err := exec.Command("launchctl", "load", plistPath).Run(); err != nil {
		return fmt.Errorf("launchctl load %s: %w", plistPath, err)
	}
	return nil
}

func (d *darwinInstaller) bootout(plistPath string) error {
	if _, err := exec.LookPath("launchctl"); err != nil {
		return nil
	}
	uid := strconv.Itoa(os.Getuid())
	// Prefer the modern bootout target. If that fails (e.g. the agent
	// was loaded with `launchctl load`), fall back to `unload`.
	// #nosec G204 -- literal binary; uid is process-local and
	// LaunchdLabel is a compile-time const.
	if err := exec.Command("launchctl", "bootout", "gui/"+uid+"/"+LaunchdLabel).Run(); err == nil {
		return nil
	}
	// #nosec G204 -- literal binary; plistPath is process-local.
	_ = exec.Command("launchctl", "unload", plistPath).Run()
	return nil
}

// ─── linux ───────────────────────────────────────────────────────────

type linuxInstaller struct {
	binary string
	home   string
}

func (l *linuxInstaller) UnitPath() (string, error) {
	return filepath.Join(l.home, ".config", "systemd", "user", SystemdUnitName), nil
}

func (l *linuxInstaller) logDir() string {
	return filepath.Join(l.home, ".local", "share", "dpkms")
}

func (l *linuxInstaller) IsInstalled() (bool, error) {
	p, _ := l.UnitPath()
	_, err := os.Stat(p)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (l *linuxInstaller) IsRunning() (bool, error) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false, nil
	}
	out, err := exec.Command("systemctl", "--user", "is-active", SystemdUnitName).Output()
	if err != nil {
		// is-active exits non-zero for "inactive" / "failed" — treat
		// those as not-running rather than as an error.
		return false, nil
	}
	return strings.TrimSpace(string(out)) == "active", nil
}

func (l *linuxInstaller) Install(force bool) error {
	if err := os.MkdirAll(l.logDir(), 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	unitPath, _ := l.UnitPath()
	if err := os.MkdirAll(filepath.Dir(unitPath), 0o755); err != nil {
		return fmt.Errorf("create systemd user dir: %w", err)
	}

	rendered, err := RenderSystemd(SystemdData{
		BinaryPath:       l.binary,
		RestartSec:       defaultRestartSec,
		StdoutPath:       filepath.Join(l.logDir(), "dpkms.out.log"),
		StderrPath:       filepath.Join(l.logDir(), "dpkms.err.log"),
		WorkingDirectory: l.home,
		HomeDir:          l.home,
	})
	if err != nil {
		return err
	}

	if force {
		_ = l.disable()
	}

	if err := os.WriteFile(unitPath, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write unit: %w", err)
	}

	return l.enable()
}

func (l *linuxInstaller) Uninstall() error {
	_ = l.disable()
	unitPath, _ := l.UnitPath()
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit: %w", err)
	}
	if _, err := exec.LookPath("systemctl"); err == nil {
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	}
	return nil
}

func (l *linuxInstaller) enable() error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemctl not found on PATH: %w", err)
	}
	if err := exec.Command("systemctl", "--user", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	if err := exec.Command("systemctl", "--user", "enable", "--now", SystemdUnitName).Run(); err != nil {
		return fmt.Errorf("systemctl enable --now %s: %w", SystemdUnitName, err)
	}
	return nil
}

func (l *linuxInstaller) disable() error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return nil
	}
	_ = exec.Command("systemctl", "--user", "disable", "--now", SystemdUnitName).Run()
	return nil
}

// ─── rendering helpers (exported for testing) ───────────────────────

// RenderPlist renders the LaunchAgent plist for the given PlistData.
// Exposed so tests can render without touching the filesystem or
// invoking launchctl.
func RenderPlist(d PlistData) (string, error) {
	if d.Label == "" {
		d.Label = LaunchdLabel
	}
	if d.ThrottleInterval == 0 {
		d.ThrottleInterval = defaultThrottleInterval
	}
	if d.PathEnv == "" {
		d.PathEnv = defaultPathEnv()
	}
	return renderTemplate("plist", templates.Plist, d)
}

// RenderSystemd renders the systemd user unit for the given SystemdData.
func RenderSystemd(d SystemdData) (string, error) {
	if d.RestartSec == 0 {
		d.RestartSec = defaultRestartSec
	}
	return renderTemplate("systemd", templates.Systemd, d)
}

func renderTemplate(name, body string, data interface{}) (string, error) {
	t, err := template.New(name).Parse(body)
	if err != nil {
		return "", fmt.Errorf("parse %s template: %w", name, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render %s template: %w", name, err)
	}
	return buf.String(), nil
}

func defaultPathEnv() string {
	// Mirrors the canonical PATH used by the m3 LaunchAgent runbook
	// so brew-installed dependencies (e.g. ffmpeg, ripgrep) are
	// reachable from the daemon.
	if v := os.Getenv("PATH"); v != "" {
		return v
	}
	return "/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
}
