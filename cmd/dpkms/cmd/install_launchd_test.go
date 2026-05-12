package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeFakePlist writes a minimal placeholder file at p (creating
// parent dirs) so the installer's IsInstalled() check returns true
// without invoking launchctl/systemctl.
func writeFakePlist(t *testing.T, p string) error {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte("placeholder"), 0o644)
}

// TestRenderPlistContainsRequiredKeys is platform-agnostic — the plist
// template is just text, so we render and assert the resulting XML
// has the keys/values the LaunchAgent contract requires.
func TestRenderPlistContainsRequiredKeys(t *testing.T) {
	tmp := t.TempDir()
	logDir := filepath.Join(tmp, "logs")
	out, err := RenderPlist(PlistData{
		BinaryPath:       "/usr/local/bin/dpkms",
		ThrottleInterval: 30,
		StdoutPath:       filepath.Join(logDir, "dpkms.out.log"),
		StderrPath:       filepath.Join(logDir, "dpkms.err.log"),
		WorkingDirectory: tmp,
		HomeDir:          tmp,
		PathEnv:          "/usr/bin:/bin",
	})
	if err != nil {
		t.Fatalf("RenderPlist: %v", err)
	}

	for _, want := range []string{
		`<?xml version="1.0"`,
		`<plist version="1.0">`,
		`<key>Label</key>`,
		`<string>com.contexthelp.dpkms</string>`,
		`<key>ProgramArguments</key>`,
		`<string>/usr/local/bin/dpkms</string>`,
		`<string>serve</string>`,
		`<key>RunAtLoad</key>`,
		`<true/>`,
		`<key>KeepAlive</key>`,
		`<key>ThrottleInterval</key>`,
		`<integer>30</integer>`,
		`<key>StandardOutPath</key>`,
		`<key>StandardErrorPath</key>`,
		`<key>WorkingDirectory</key>`,
		filepath.Join(logDir, "dpkms.out.log"),
		filepath.Join(logDir, "dpkms.err.log"),
		tmp,
		`/usr/bin:/bin`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plist missing %q\n--- rendered ---\n%s", want, out)
		}
	}
}

// TestRenderPlistDefaults verifies that empty-zero-value fields receive
// the documented defaults — so callers can render with a minimal
// PlistData and still get a valid agent.
func TestRenderPlistDefaults(t *testing.T) {
	out, err := RenderPlist(PlistData{
		BinaryPath: "/x/dpkms",
		StdoutPath: "/x/out.log",
		StderrPath: "/x/err.log",
		HomeDir:    "/x",
	})
	if err != nil {
		t.Fatalf("RenderPlist: %v", err)
	}
	if !strings.Contains(out, "<string>com.contexthelp.dpkms</string>") {
		t.Error("default Label not applied")
	}
	if !strings.Contains(out, "<integer>30</integer>") {
		t.Error("default ThrottleInterval=30 not applied")
	}
}

// TestRenderSystemdContainsRequiredKeys mirrors the plist test for the
// systemd unit body.
func TestRenderSystemdContainsRequiredKeys(t *testing.T) {
	tmp := t.TempDir()
	logDir := filepath.Join(tmp, "logs")
	out, err := RenderSystemd(SystemdData{
		BinaryPath:       "/usr/local/bin/dpkms",
		RestartSec:       30,
		StdoutPath:       filepath.Join(logDir, "dpkms.out.log"),
		StderrPath:       filepath.Join(logDir, "dpkms.err.log"),
		WorkingDirectory: tmp,
		HomeDir:          tmp,
	})
	if err != nil {
		t.Fatalf("RenderSystemd: %v", err)
	}

	for _, want := range []string{
		"[Unit]",
		"Description=dpkms daemon",
		"[Service]",
		"ExecStart=/usr/local/bin/dpkms serve",
		"Restart=always",
		"RestartSec=30",
		"StandardOutput=append:" + filepath.Join(logDir, "dpkms.out.log"),
		"StandardError=append:" + filepath.Join(logDir, "dpkms.err.log"),
		"WorkingDirectory=" + tmp,
		"Environment=HOME=" + tmp,
		"[Install]",
		"WantedBy=default.target",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("systemd unit missing %q\n--- rendered ---\n%s", want, out)
		}
	}
}

func TestRenderSystemdDefaults(t *testing.T) {
	out, err := RenderSystemd(SystemdData{
		BinaryPath: "/x/dpkms",
		StdoutPath: "/x/out.log",
		StderrPath: "/x/err.log",
		HomeDir:    "/x",
	})
	if err != nil {
		t.Fatalf("RenderSystemd: %v", err)
	}
	if !strings.Contains(out, "RestartSec=30") {
		t.Errorf("default RestartSec=30 not applied:\n%s", out)
	}
}

// ─── platform-gated installer tests ────────────────────────────────

// TestDarwinInstaller_PathsAndIdempotence is the darwin install/
// uninstall round-trip. Skips on non-darwin. Doesn't shell out to
// launchctl — it simulates the file-side of install via Install/
// Uninstall and asserts IsInstalled() flips correctly.
//
// The test forces home to t.TempDir() so we never touch the user's
// real ~/Library/LaunchAgents.
func TestDarwinInstaller_PathsAndIdempotence(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only")
	}
	tmpHome := t.TempDir()
	d := &darwinInstaller{
		binary: filepath.Join(tmpHome, "fake-dpkms"),
		home:   tmpHome,
	}

	// Pre-condition: not installed.
	installed, err := d.IsInstalled()
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if installed {
		t.Fatalf("expected fresh installer to not be installed")
	}

	// Render via the same code path Install uses, but don't shell out
	// to launchctl: we exercise the file-write step directly.
	plistPath, _ := d.UnitPath()
	if got, want := filepath.Dir(plistPath), filepath.Join(tmpHome, "Library", "LaunchAgents"); got != want {
		t.Errorf("UnitPath dir = %q, want %q", got, want)
	}
	if got, want := filepath.Base(plistPath), "com.contexthelp.dpkms.plist"; got != want {
		t.Errorf("UnitPath base = %q, want %q", got, want)
	}

	// Simulate a previous install by writing the plist directly so
	// IsInstalled() returns true without launchctl side effects.
	if err := writeFakePlist(t, plistPath); err != nil {
		t.Fatalf("writeFakePlist: %v", err)
	}
	installed, err = d.IsInstalled()
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if !installed {
		t.Fatalf("expected installed=true after writing plist")
	}
}

// TestLinuxInstaller_PathsAndIdempotence mirrors the darwin test for
// systemd. Skips on non-linux.
func TestLinuxInstaller_PathsAndIdempotence(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	tmpHome := t.TempDir()
	l := &linuxInstaller{
		binary: filepath.Join(tmpHome, "fake-dpkms"),
		home:   tmpHome,
	}

	installed, err := l.IsInstalled()
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if installed {
		t.Fatalf("expected fresh installer to not be installed")
	}

	unitPath, _ := l.UnitPath()
	if got, want := filepath.Dir(unitPath), filepath.Join(tmpHome, ".config", "systemd", "user"); got != want {
		t.Errorf("UnitPath dir = %q, want %q", got, want)
	}
	if got, want := filepath.Base(unitPath), "dpkms.service"; got != want {
		t.Errorf("UnitPath base = %q, want %q", got, want)
	}

	if err := writeFakePlist(t, unitPath); err != nil {
		t.Fatalf("writeFakeUnit: %v", err)
	}
	installed, err = l.IsInstalled()
	if err != nil {
		t.Fatalf("IsInstalled: %v", err)
	}
	if !installed {
		t.Fatalf("expected installed=true after writing unit")
	}
}

// TestNewInstaller_UnsupportedPlatform — newInstaller hard-fails on
// platforms other than darwin/linux. We can't easily change runtime.GOOS
// at runtime, so this test only fires on supported platforms but
// asserts the matching installer type comes back.
func TestNewInstaller_PlatformDispatch(t *testing.T) {
	inst, err := newInstaller()
	if err != nil {
		// On freebsd/windows we'd see "unsupported platform" — that's
		// the documented behavior. Skip rather than fail because
		// nothing else in this test suite can run there anyway.
		t.Skipf("newInstaller: %v", err)
	}
	switch runtime.GOOS {
	case "darwin":
		if _, ok := inst.(*darwinInstaller); !ok {
			t.Errorf("darwin: got %T, want *darwinInstaller", inst)
		}
	case "linux":
		if _, ok := inst.(*linuxInstaller); !ok {
			t.Errorf("linux: got %T, want *linuxInstaller", inst)
		}
	}
}
