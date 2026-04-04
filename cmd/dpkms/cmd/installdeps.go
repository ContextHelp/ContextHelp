package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/deps"
	_ "github.com/ideacrafterslabs/ctxt/internal/providers" // register builtin deps
	"github.com/spf13/cobra"
)

var installDepsCmd = &cobra.Command{
	Use:    "install-deps",
	Short:  "Install all registered runtime dependencies",
	Hidden: true, // invoked by 'make deps', not shown in help
	Long: `Install system and Python packages required by built-in pipelines
and any registered plugins or custom detectors.

Package manager detection order: brew → apt → dnf → pacman
Python packages are installed via pip3 or pip.`,
	RunE: runInstallDeps,
}

func init() {
	rootCmd.AddCommand(installDepsCmd)
}

func runInstallDeps(cmd *cobra.Command, args []string) error {
	pkgMgr := detectPackageManager()
	pip := detectPip()

	var failed []string
	for _, d := range deps.All() {
		if err := installDep(d, pkgMgr, pip); err != nil {
			fmt.Fprintf(os.Stderr, "  ✗ %s: %v\n", d.Binary, err)
			failed = append(failed, d.Binary)
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("failed to install: %s", strings.Join(failed, ", "))
	}
	fmt.Println("✓ All dependencies installed")
	return nil
}

func installDep(d deps.Dep, pkgMgr, pip string) error {
	if toolInstalled(d.Binary) {
		fmt.Printf("✓ %s already installed (%s)\n", d.Binary, d.Description)
		return nil
	}

	// Python-only dep (no system package defined): go straight to pip.
	if d.PipPkg != "" && d.BrewPkg == "" && d.AptPkg == "" {
		return installPip(d.PipPkg, d.Binary, pip)
	}

	// System package manager available.
	if pkgMgr != "" {
		return installSystem(d, pkgMgr)
	}

	// Fallback to pip if a PyPI package is defined.
	if d.PipPkg != "" {
		return installPip(d.PipPkg, d.Binary, pip)
	}

	return fmt.Errorf("no supported package manager found (brew/apt/dnf/pacman); install %s manually", d.Binary)
}

func installSystem(d deps.Dep, mgr string) error {
	var pkg string
	switch mgr {
	case "brew":
		pkg = d.BrewPkg
	case "apt-get":
		pkg = d.AptPkg
	case "dnf":
		pkg = d.DnfPkg
	case "pacman":
		pkg = d.PacmanPkg
	case "winget", "choco":
		pkg = d.BrewPkg // best-effort: fall back to brew pkg name; callers should set WingetPkg when available
	}
	if pkg == "" {
		// This manager has no package defined; try pip if available.
		if d.PipPkg != "" {
			pip := detectPip()
			return installPip(d.PipPkg, d.Binary, pip)
		}
		return fmt.Errorf("no package defined for manager %q; install %s manually", mgr, d.Binary)
	}

	args := packageManagerArgs(mgr, pkg)
	fmt.Printf("  Installing %s via %s...\n", pkg, mgr)
	return runInstallCmd(args[0], args[1:]...)
}

func installPip(pipPkg, binary, pip string) error {
	if pip == "" {
		return fmt.Errorf("pip not found; install Python first (https://python.org) then: pip install %s", pipPkg)
	}
	fmt.Printf("  Installing %s via %s...\n", pipPkg, pip)
	return runInstallCmd(pip, "install", pipPkg)
}

func packageManagerArgs(mgr, pkg string) []string {
	switch mgr {
	case "brew":
		return []string{"brew", "install", pkg}
	case "apt-get":
		return []string{"sudo", "apt-get", "install", "-y", pkg}
	case "dnf":
		return []string{"sudo", "dnf", "install", "-y", pkg}
	case "pacman":
		return []string{"sudo", "pacman", "-S", "--noconfirm", pkg}
	case "zypper":
		return []string{"sudo", "zypper", "install", "-y", pkg}
	case "winget":
		return []string{"winget", "install", "--id", pkg, "-e", "--silent"}
	case "choco":
		return []string{"choco", "install", "-y", pkg}
	}
	return nil
}

func runInstallCmd(name string, args ...string) error {
	cmd := exec.CommandContext(context.Background(), name, args...) // #nosec G204 -- caller controls command
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func detectPackageManager() string {
	switch runtime.GOOS {
	case "darwin":
		if toolInstalled("brew") {
			return "brew"
		}
		return ""
	case "windows":
		for _, mgr := range []string{"winget", "choco"} {
			if toolInstalled(mgr) {
				return mgr
			}
		}
		return ""
	default: // linux and others
		for _, mgr := range []string{"apt-get", "dnf", "pacman", "zypper"} {
			if toolInstalled(mgr) {
				return mgr
			}
		}
		return ""
	}
}

// platformExtraDirs returns platform-specific directories to probe when
// exec.LookPath misses a binary (e.g. tools installed outside PATH at build time).
func platformExtraDirs(home string) []string {
	switch runtime.GOOS {
	case "windows":
		return []string{
			filepath.Join(os.Getenv("ProgramFiles"), "Git", "usr", "bin"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Packages"),
			filepath.Join(home, "scoop", "shims"),
			filepath.Join(home, "AppData", "Local", "Programs"),
		}
	default:
		return []string{
			"/opt/homebrew/bin",
			"/usr/local/bin",
			filepath.Join(home, ".local", "bin"),
		}
	}
}

func detectPip() string {
	for _, p := range []string{"pip3", "pip"} {
		if toolInstalled(p) {
			return p
		}
	}
	return ""
}

func toolInstalled(name string) bool {
	if _, err := exec.LookPath(name); err == nil {
		return true
	}
	home, _ := os.UserHomeDir()
	for _, dir := range platformExtraDirs(home) {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}
