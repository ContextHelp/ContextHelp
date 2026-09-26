package chromium

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestDefaultUserDataDirPerGOOS(t *testing.T) {
	const (
		macHome = "/Users/alice"
		lnxHome = "/home/alice"
		winHome = `C:\Users\alice`
		winLAD  = `C:\Users\alice\AppData\Local`
	)
	mac := macHome + "/Library/Application Support/"

	tests := []struct {
		name    string
		browser Browser
		goos    string
		home    string
		env     map[string]string
		want    string
		wantErr error
	}{
		// darwin
		{"chrome darwin", Chrome, "darwin", macHome, nil, mac + "Google/Chrome", nil},
		{"brave darwin", Brave, "darwin", macHome, nil, mac + "BraveSoftware/Brave-Browser", nil},
		{"edge darwin", Edge, "darwin", macHome, nil, mac + "Microsoft Edge", nil},
		{"arc darwin", Arc, "darwin", macHome, nil, mac + "Arc/User Data", nil},
		{"chromium darwin", Chromium, "darwin", macHome, nil, mac + "Chromium", nil},
		{"vivaldi darwin", Vivaldi, "darwin", macHome, nil, mac + "Vivaldi", nil},

		// linux, default ~/.config
		{"chrome linux", Chrome, "linux", lnxHome, nil, lnxHome + "/.config/google-chrome", nil},
		{"brave linux", Brave, "linux", lnxHome, nil, lnxHome + "/.config/BraveSoftware/Brave-Browser", nil},
		{"edge linux", Edge, "linux", lnxHome, nil, lnxHome + "/.config/microsoft-edge", nil},
		{"chromium linux", Chromium, "linux", lnxHome, nil, lnxHome + "/.config/chromium", nil},
		{"vivaldi linux", Vivaldi, "linux", lnxHome, nil, lnxHome + "/.config/vivaldi", nil},
		{"arc linux unsupported", Arc, "linux", lnxHome, nil, "", ErrUnsupported},

		// linux, XDG_CONFIG_HOME and CHROME_CONFIG_HOME
		{
			"brave linux xdg", Brave, "linux", lnxHome,
			map[string]string{"XDG_CONFIG_HOME": "/xdg"},
			"/xdg/BraveSoftware/Brave-Browser", nil,
		},
		{
			"chrome linux chrome_config_home beats xdg", Chrome, "linux", lnxHome,
			map[string]string{"XDG_CONFIG_HOME": "/xdg", "CHROME_CONFIG_HOME": "/cch"},
			"/cch/google-chrome", nil,
		},
		{
			"chromium linux chrome_config_home", Chromium, "linux", lnxHome,
			map[string]string{"CHROME_CONFIG_HOME": "/cch"},
			"/cch/chromium", nil,
		},
		{
			"brave linux ignores chrome_config_home", Brave, "linux", lnxHome,
			map[string]string{"CHROME_CONFIG_HOME": "/cch"},
			lnxHome + "/.config/BraveSoftware/Brave-Browser", nil,
		},

		// windows
		{
			"chrome windows", Chrome, "windows", winHome,
			map[string]string{"LOCALAPPDATA": winLAD},
			winLAD + `\Google\Chrome\User Data`, nil,
		},
		{
			"brave windows", Brave, "windows", winHome,
			map[string]string{"LOCALAPPDATA": winLAD},
			winLAD + `\BraveSoftware\Brave-Browser\User Data`, nil,
		},
		{
			"edge windows", Edge, "windows", winHome,
			map[string]string{"LOCALAPPDATA": winLAD},
			winLAD + `\Microsoft\Edge\User Data`, nil,
		},
		{
			"chromium windows", Chromium, "windows", winHome,
			map[string]string{"LOCALAPPDATA": winLAD},
			winLAD + `\Chromium\User Data`, nil,
		},
		{
			"vivaldi windows", Vivaldi, "windows", winHome,
			map[string]string{"LOCALAPPDATA": winLAD},
			winLAD + `\Vivaldi\User Data`, nil,
		},
		{
			"windows without LOCALAPPDATA falls back to home", Chrome, "windows", winHome,
			nil, winHome + `\AppData\Local\Google\Chrome\User Data`, nil,
		},
		{"arc windows unsupported", Arc, "windows", winHome, nil, "", ErrUnsupported},

		// errors
		{"unknown os", Chrome, "plan9", "/usr/alice", nil, "", ErrUnsupported},
		{"unknown browser", Browser("netscape"), "darwin", macHome, nil, "", ErrUnknownBrowser},
		{"no home", Chrome, "darwin", "", nil, "", ErrNoHome},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(k string) string { return tt.env[k] }
			got, err := defaultUserDataDir(tt.browser, tt.goos, getenv, tt.home)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUserDataDirOverrides(t *testing.T) {
	envDir := filepath.Join(t.TempDir(), "env")
	optDir := filepath.Join(t.TempDir(), "opt")

	t.Run("env override", func(t *testing.T) {
		t.Setenv("CTXT_BRAVE_USER_DATA_DIR", envDir)
		got, err := UserDataDir(Brave)
		if err != nil {
			t.Fatalf("UserDataDir: %v", err)
		}
		if got != envDir {
			t.Fatalf("got %q, want %q", got, envDir)
		}
	})

	t.Run("option beats env", func(t *testing.T) {
		t.Setenv("CTXT_BRAVE_USER_DATA_DIR", envDir)
		got, err := UserDataDir(Brave, WithUserDataDir(optDir))
		if err != nil {
			t.Fatalf("UserDataDir: %v", err)
		}
		if got != optDir {
			t.Fatalf("got %q, want %q", got, optDir)
		}
	})

	t.Run("option on browser unsupported here", func(t *testing.T) {
		// An explicit dir makes the browser usable even where no default
		// location is known (e.g. a portable install).
		got, err := UserDataDir(Arc, WithUserDataDir(optDir))
		if err != nil {
			t.Fatalf("UserDataDir: %v", err)
		}
		if got != optDir {
			t.Fatalf("got %q, want %q", got, optDir)
		}
	})

	t.Run("unknown browser rejected even with option", func(t *testing.T) {
		_, err := UserDataDir(Browser("netscape"), WithUserDataDir(optDir))
		if !errors.Is(err, ErrUnknownBrowser) {
			t.Fatalf("err = %v, want ErrUnknownBrowser", err)
		}
	})
}

func TestParseBrowser(t *testing.T) {
	for _, in := range []string{"chrome", "Brave", " EDGE ", "arc", "chromium", "vivaldi"} {
		if _, err := ParseBrowser(in); err != nil {
			t.Errorf("ParseBrowser(%q): %v", in, err)
		}
	}
	if _, err := ParseBrowser("netscape"); !errors.Is(err, ErrUnknownBrowser) {
		t.Fatalf("err = %v, want ErrUnknownBrowser", err)
	}
}
