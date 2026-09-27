package dpkmsclient_test

import (
	"errors"
	"strings"
	"testing"

	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
)

// laptop is the ADR-077 example config: a default token, a named remote
// with its own token, a named local entry without one, and an unnamed
// entry. server.url is set too, so a test can see it lose to server.urls.
func laptop() config.ServerConfig {
	return config.ServerConfig{
		Token: "tok-default",
		URL:   "http://single.example.net:7000",
		URLs: []config.ServerEndpoint{
			{Name: "home", URL: "https://dpkms.example.ts.net:7700", Token: "tok-home"},
			{Name: "laptop", URL: "http://127.0.0.1:8080"},
			{URL: "https://Other.Example.NET:443/"},
		},
	}
}

// locals returns a fixed set of running local instances.
func locals(infos ...pidfile.Info) func() ([]pidfile.Info, error) {
	return func() ([]pidfile.Info, error) { return infos, nil }
}

// noLocals fails the test if the resolver scans pidfiles: the layer under
// test must not need them.
func noLocals(t *testing.T) func() ([]pidfile.Info, error) {
	return func() ([]pidfile.Info, error) {
		t.Helper()
		t.Error("resolver scanned local instances; the selected layer must not need them")
		return nil, nil
	}
}

func resolve(t *testing.T, sc config.ServerConfig, sel dpkmsclient.Selection, loc func() ([]pidfile.Info, error)) dpkmsclient.Resolved {
	t.Helper()
	r, err := dpkmsclient.Resolve(sc, sel, loc)
	if err != nil {
		t.Fatalf("Resolve(%+v): %v", sel, err)
	}
	return r
}

func TestResolvePrecedence(t *testing.T) {
	work := pidfile.Info{Name: "work", Port: 8093}
	cases := []struct {
		name string
		sc   config.ServerConfig
		sel  dpkmsclient.Selection
		loc  []pidfile.Info
		want dpkmsclient.Resolved
	}{
		{
			name: "--server beats --instance and state",
			sc:   laptop(),
			sel: dpkmsclient.Selection{
				Server: "http://pinned.example.net:9000", Instance: "home",
				InstanceLayer: dpkmsclient.LayerInstanceFlag, Current: "laptop",
			},
			want: dpkmsclient.Resolved{
				Endpoint: dpkmsclient.Endpoint{URL: "http://pinned.example.net:9000", Token: "tok-default"},
				Key:      "http://pinned.example.net:9000", Layer: dpkmsclient.LayerServerFlag,
			},
		},
		{
			name: "--server naming a configured entry reuses its token and name",
			sc:   laptop(),
			sel:  dpkmsclient.Selection{Server: "https://dpkms.example.ts.net:7700/"},
			want: dpkmsclient.Resolved{
				Endpoint: dpkmsclient.Endpoint{URL: "https://dpkms.example.ts.net:7700/", Token: "tok-home"},
				Name:     "home", Key: "home", Layer: dpkmsclient.LayerServerFlag,
			},
		},
		{
			name: "--instance beats state",
			sc:   laptop(),
			sel: dpkmsclient.Selection{
				Instance: "home", InstanceLayer: dpkmsclient.LayerInstanceFlag, Current: "laptop",
			},
			want: dpkmsclient.Resolved{
				Endpoint: dpkmsclient.Endpoint{URL: "https://dpkms.example.ts.net:7700", Token: "tok-home"},
				Name:     "home", Key: "home", Layer: dpkmsclient.LayerInstanceFlag,
			},
		},
		{
			name: "CTXT_INSTANCE reports its own layer; an entry without a token takes server.token",
			sc:   laptop(),
			sel:  dpkmsclient.Selection{Instance: "laptop", InstanceLayer: dpkmsclient.LayerInstanceEnv},
			want: dpkmsclient.Resolved{
				Endpoint: dpkmsclient.Endpoint{URL: "http://127.0.0.1:8080", Token: "tok-default"},
				Name:     "laptop", Key: "laptop", Layer: dpkmsclient.LayerInstanceEnv,
			},
		},
		{
			name: "state beats server.urls",
			sc:   laptop(),
			sel:  dpkmsclient.Selection{Current: "home"},
			want: dpkmsclient.Resolved{
				Endpoint: dpkmsclient.Endpoint{URL: "https://dpkms.example.ts.net:7700", Token: "tok-home"},
				Name:     "home", Key: "home", Layer: dpkmsclient.LayerCurrent,
			},
		},
		{
			name: "no selection: the first server.urls entry, never another",
			sc:   laptop(),
			want: dpkmsclient.Resolved{
				Endpoint: dpkmsclient.Endpoint{URL: "https://dpkms.example.ts.net:7700", Token: "tok-home"},
				Name:     "home", Key: "home", Layer: dpkmsclient.LayerURLs,
			},
		},
		{
			name: "unnamed first entry is keyed by its normalized URL",
			sc: config.ServerConfig{URLs: []config.ServerEndpoint{
				{URL: "HTTPS://Other.Example.NET:443/"}, {Name: "home", URL: "https://h.example.net"},
			}},
			want: dpkmsclient.Resolved{
				Endpoint: dpkmsclient.Endpoint{URL: "HTTPS://Other.Example.NET:443/"},
				Key:      "https://other.example.net", Layer: dpkmsclient.LayerURLs,
			},
		},
		{
			name: "server.url without server.urls",
			sc:   config.ServerConfig{URL: "http://single.example.net:7000", Token: "tok-default"},
			want: dpkmsclient.Resolved{
				Endpoint: dpkmsclient.Endpoint{URL: "http://single.example.net:7000", Token: "tok-default"},
				Key:      "http://single.example.net:7000", Layer: dpkmsclient.LayerURL,
			},
		},
		{
			name: "nothing configured: the default",
			sc:   config.ServerConfig{Token: "tok-default"},
			want: dpkmsclient.Resolved{
				Endpoint: dpkmsclient.Endpoint{URL: dpkmsclient.DefaultURL, Token: "tok-default"},
				Key:      dpkmsclient.DefaultURL, Layer: dpkmsclient.LayerDefault,
			},
		},
		{
			name: "a name no entry has selects a running local instance by name",
			sc:   laptop(),
			sel:  dpkmsclient.Selection{Instance: "work", InstanceLayer: dpkmsclient.LayerInstanceFlag},
			loc:  []pidfile.Info{work},
			want: dpkmsclient.Resolved{
				Endpoint: dpkmsclient.Endpoint{URL: "http://127.0.0.1:8093", Token: "tok-default"},
				Name:     "work", Key: "http://127.0.0.1:8093", Layer: dpkmsclient.LayerInstanceFlag, Local: true,
			},
		},
		{
			name: "a port selects a running local instance",
			sc:   laptop(),
			sel:  dpkmsclient.Selection{Current: "8093"},
			loc:  []pidfile.Info{work},
			want: dpkmsclient.Resolved{
				Endpoint: dpkmsclient.Endpoint{URL: "http://127.0.0.1:8093", Token: "tok-default"},
				Name:     "work", Key: "http://127.0.0.1:8093", Layer: dpkmsclient.LayerCurrent, Local: true,
			},
		},
		{
			name: "a named entry wins over a local instance of the same name",
			sc:   laptop(),
			sel:  dpkmsclient.Selection{Instance: "home", InstanceLayer: dpkmsclient.LayerInstanceFlag},
			loc:  []pidfile.Info{{Name: "home", Port: 8094}},
			want: dpkmsclient.Resolved{
				Endpoint: dpkmsclient.Endpoint{URL: "https://dpkms.example.ts.net:7700", Token: "tok-home"},
				Name:     "home", Key: "home", Layer: dpkmsclient.LayerInstanceFlag,
			},
		},
		{
			name: "a local instance at a configured entry's URL takes that entry's token and key",
			sc: config.ServerConfig{Token: "tok-default", URLs: []config.ServerEndpoint{
				{Name: "box", URL: "http://127.0.0.1:8093/", Token: "tok-box"},
			}},
			sel: dpkmsclient.Selection{Instance: "8093", InstanceLayer: dpkmsclient.LayerInstanceFlag},
			loc: []pidfile.Info{work},
			want: dpkmsclient.Resolved{
				Endpoint: dpkmsclient.Endpoint{URL: "http://127.0.0.1:8093", Token: "tok-box"},
				Name:     "work", Key: "box", Layer: dpkmsclient.LayerInstanceFlag, Local: true,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loc := noLocals(t)
			if tc.loc != nil {
				loc = locals(tc.loc...)
			}
			got := resolve(t, tc.sc, tc.sel, loc)
			if got != tc.want {
				t.Errorf("Resolve =\n  %+v\nwant\n  %+v", got, tc.want)
			}
		})
	}
}

// TestResolveUnknownNameIsPrerequisite: a name that is neither a
// configured entry nor a running local instance exits 70, and the fix
// lists what could have been meant. Nothing falls through to server.urls.
func TestResolveUnknownNameIsPrerequisite(t *testing.T) {
	for _, sel := range []dpkmsclient.Selection{
		{Instance: "nope", InstanceLayer: dpkmsclient.LayerInstanceFlag},
		{Instance: "nope", InstanceLayer: dpkmsclient.LayerInstanceEnv},
		{Current: "nope"},
	} {
		_, err := dpkmsclient.Resolve(laptop(), sel, locals(pidfile.Info{Name: "work", Port: 8093}))
		var e *output.Error
		if !errors.As(err, &e) {
			t.Fatalf("Resolve(%+v) error = %v; want a kit envelope", sel, err)
		}
		if e.ExitCode != output.ExitPrerequisite || e.Code != output.CodePrerequisite {
			t.Errorf("Resolve(%+v) = %s/%d; want %s/%d", sel, e.Code, e.ExitCode,
				output.CodePrerequisite, output.ExitPrerequisite)
		}
		if !strings.Contains(e.Message, `"nope"`) {
			t.Errorf("message %q does not name the selection", e.Message)
		}
		for _, want := range []string{"home", "laptop", "work (port 8093)"} {
			if !strings.Contains(e.SuggestedFix, want) {
				t.Errorf("SuggestedFix %q lacks %q", e.SuggestedFix, want)
			}
		}
		if sel.Current != "" && !strings.Contains(e.SuggestedFix, "ctxt instance use -") {
			t.Errorf("stale state fix %q does not say how to clear it", e.SuggestedFix)
		}
	}
}

// TestResolveLocalScanFailure: a failed pidfile scan for a name no entry
// has is an error, never a silent fall-through to server.urls.
func TestResolveLocalScanFailure(t *testing.T) {
	boom := errors.New("run dir unreadable")
	_, err := dpkmsclient.Resolve(laptop(),
		dpkmsclient.Selection{Instance: "work", InstanceLayer: dpkmsclient.LayerInstanceFlag},
		func() ([]pidfile.Info, error) { return nil, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("Resolve error = %v; want it to wrap %v", err, boom)
	}
}

func TestNormalizeURL(t *testing.T) {
	for in, want := range map[string]string{
		"http://127.0.0.1:8080":            "http://127.0.0.1:8080",
		"http://127.0.0.1:8080/":           "http://127.0.0.1:8080",
		"HTTP://LocalHost:80/":             "http://localhost",
		"https://Box.Example.NET:443/api/": "https://box.example.net/api",
		"https://box.example.net:7700":     "https://box.example.net:7700",
	} {
		if got := dpkmsclient.NormalizeURL(in); got != want {
			t.Errorf("NormalizeURL(%q) = %q; want %q", in, got, want)
		}
	}
}
