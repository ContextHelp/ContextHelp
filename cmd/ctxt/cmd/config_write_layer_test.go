package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Config-changing commands edit only the target file's own layer. Values
// from other files, `-c key=value` overrides, env vars and built-in
// defaults must never be written into it, and every byte the edit does not
// touch (comments, blank lines, key order, quoting, flow style) survives.

// ownLayerFixture is the target file every scenario starts from. {DB} is
// replaced with the test's sqlite path.
const ownLayerFixture = `# ctxt config, hand edited
storage:
  type: sqlite # local
  path: {DB}

# focus profiles
profile:
  default: work
  profiles:
    work:
      description: Work stuff
      tags: [client, billing]
    founder:
      description: 'Founder hat'
      schema:
        version: 2
        entity_types: [decision, risk]
        topic_vocabulary:
          - pricing
          - security
        classification_rules: []

registries:
  - name: alpha
    url: https://alpha.example
  - name: beta # keep
    url: https://beta.example

watch:
  clipboard:
    enabled: false
    min_length: 40
`

// ownLayerEnv is one isolated config world: a user file, an optional -c
// overlay file, a project file under cwd, env values and a -c key=value.
type ownLayerEnv struct {
	home    string
	db      string
	user    string // $XDG_CONFIG_HOME/contexthelp/ctxt.yaml
	overlay string // -c <path>; empty when the scenario passes none
	project string // <cwd>/.contexthelp/ctxt.yaml
	target  string // the file the command must edit
	before  map[string][]byte
}

// newOwnLayerEnv builds the world. With viaOverlay the fixture lives in
// the -c overlay file (the last -c path is the write target); otherwise it
// lives in the user file and no -c path is passed.
func newOwnLayerEnv(t *testing.T, viaOverlay bool) *ownLayerEnv {
	t.Helper()
	db := setupTestDB(t) // isolates HOME + XDG to a temp dir
	home := filepath.Dir(db.ConfigPath)
	e := &ownLayerEnv{
		home:    home,
		db:      filepath.Join(home, "test.db"),
		user:    filepath.Join(home, "contexthelp", "ctxt.yaml"), // the XDG user slot
		project: filepath.Join(home, "proj", ".contexthelp", "ctxt.yaml"),
	}

	fixture := strings.ReplaceAll(ownLayerFixture, "{DB}", e.db)
	userBody := "# user layer\nserver:\n  url: http://127.0.0.1:1\n"
	if viaOverlay {
		e.overlay = filepath.Join(home, "overlay", "ctxt.yaml")
		e.target = e.overlay
		writeFile(t, e.overlay, fixture)
	} else {
		e.target = e.user
		userBody = fixture + "server:\n  url: http://127.0.0.1:1\n"
	}
	writeFile(t, e.user, userBody)
	writeFile(t, e.project,
		"profile:\n  profiles:\n    projonly:\n      description: from the project layer\n"+
			"registries_global:\n  require_signatures: false\n")

	// CTXT_CONFIG would short-circuit the cascade and drop the project
	// layer; the XDG user slot keeps all three file layers in play.
	t.Setenv("CTXT_CONFIG", "")
	t.Setenv("CH_SERVER_PORT", "9999")
	t.Setenv("CH_GRPC_PORT", "9998")
	t.Chdir(filepath.Dir(filepath.Dir(e.project)))

	e.before = map[string][]byte{}
	for _, p := range []string{e.user, e.overlay, e.project} {
		if p == "" {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		e.before[p] = data
	}
	return e
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// run executes a command with a -c key=value override (and the overlay
// path when the scenario has one) on top of the env values.
func (e *ownLayerEnv) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	full := []string{}
	if e.overlay != "" {
		full = append(full, "--config", e.overlay)
	}
	full = append(full, "--config", "watch.clipboard.min_length=123")
	full = append(full, args...)
	return executeCommand(full...)
}

// assertTarget checks the target file equals want, and that every other
// config file is byte-identical to its starting content.
func (e *ownLayerEnv) assertTarget(t *testing.T, want string) {
	t.Helper()
	got, err := os.ReadFile(e.target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != want {
		t.Errorf("target file mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	for p, before := range e.before {
		if p == e.target {
			continue
		}
		after, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		if string(after) != string(before) {
			t.Errorf("%s changed; only the target may change\n--- got ---\n%s", p, after)
		}
	}
}

// targetStart is the target file's starting content.
func (e *ownLayerEnv) targetStart() string {
	return string(e.before[e.target])
}

// edit applies one textual replacement to the target's starting content,
// failing when old does not occur exactly once.
func edit(t *testing.T, s, old, repl string) string {
	t.Helper()
	if n := strings.Count(s, old); n != 1 {
		t.Fatalf("fixture snippet %q occurs %d times; want 1", old, n)
	}
	return strings.Replace(s, old, repl, 1)
}

// TestConfigWriteCommandsEditOnlyOwnLayer: per command, the target file
// gains exactly the intended change and nothing merged in from the other
// layers, the -c override, env or defaults.
func TestConfigWriteCommandsEditOnlyOwnLayer(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want func(t *testing.T, start string) string
	}{
		{
			name: "profile create",
			args: []string{"profile", "create", "neo"},
			want: func(t *testing.T, s string) string {
				return edit(t, s, "        classification_rules: []\n",
					"        classification_rules: []\n    neo:\n      description: Profile for neo\n")
			},
		},
		{
			name: "profile rm",
			args: []string{"profile", "rm", "--confirm=yes", "founder"},
			want: func(t *testing.T, s string) string {
				return edit(t, s, `    founder:
      description: 'Founder hat'
      schema:
        version: 2
        entity_types: [decision, risk]
        topic_vocabulary:
          - pricing
          - security
        classification_rules: []
`, "")
			},
		},
		{
			name: "profile rm of the default clears the pointer",
			args: []string{"profile", "rm", "--confirm=yes", "work"},
			want: func(t *testing.T, s string) string {
				s = edit(t, s, "  default: work\n", "  default: \"\"\n")
				return edit(t, s, "    work:\n      description: Work stuff\n      tags: [client, billing]\n", "")
			},
		},
		{
			name: "profile set",
			args: []string{"profile", "set", "founder"},
			want: func(t *testing.T, s string) string {
				return edit(t, s, "  default: work\n", "  default: founder\n")
			},
		},
		{
			name: "profile unset",
			args: []string{"profile", "unset", "work"},
			want: func(t *testing.T, s string) string {
				return edit(t, s, "  default: work\n", "  default: \"\"\n")
			},
		},
		{
			name: "profile default (deprecated) pins",
			args: []string{"profile", "default", "founder"},
			want: func(t *testing.T, s string) string {
				return edit(t, s, "  default: work\n", "  default: founder\n")
			},
		},
		{
			name: "profile default (deprecated) clears",
			args: []string{"profile", "default"},
			want: func(t *testing.T, s string) string {
				return edit(t, s, "  default: work\n", "  default: \"\"\n")
			},
		},
		{
			name: "profile schema add-type",
			args: []string{"profile", "schema", "add-type", "founder", "task"},
			want: func(t *testing.T, s string) string {
				s = edit(t, s, "        version: 2\n", "        version: 3\n")
				return edit(t, s, "[decision, risk]", "[decision, risk, task]")
			},
		},
		{
			name: "profile schema add-topic",
			args: []string{"profile", "schema", "add-topic", "founder", "ops"},
			want: func(t *testing.T, s string) string {
				s = edit(t, s, "        version: 2\n", "        version: 3\n")
				return edit(t, s, "          - security\n", "          - security\n          - ops\n")
			},
		},
		{
			name: "profile schema add-rule",
			args: []string{"profile", "schema", "add-rule", "founder", "(?i)deploy", "task"},
			want: func(t *testing.T, s string) string {
				s = edit(t, s, "        version: 2\n", "        version: 3\n")
				return edit(t, s, "        classification_rules: []\n",
					"        classification_rules:\n          - pattern: (?i)deploy\n            type: task\n")
			},
		},
		{
			name: "profile schema remove-type",
			args: []string{"profile", "schema", "remove-type", "--confirm=yes", "founder", "risk"},
			want: func(t *testing.T, s string) string {
				s = edit(t, s, "        version: 2\n", "        version: 3\n")
				return edit(t, s, "[decision, risk]", "[decision]")
			},
		},
		{
			name: "profile schema remove-topic",
			args: []string{"profile", "schema", "remove-topic", "--confirm=yes", "founder", "security"},
			want: func(t *testing.T, s string) string {
				s = edit(t, s, "        version: 2\n", "        version: 3\n")
				return edit(t, s, "          - security\n", "")
			},
		},
		{
			name: "registry delete",
			args: []string{"registry", "delete", "--confirm=yes", "beta"},
			want: func(t *testing.T, s string) string {
				return edit(t, s, "  - name: beta # keep\n    url: https://beta.example\n", "")
			},
		},
		{
			name: "watch enable",
			args: []string{"watch", "enable", "clipboard"},
			want: func(t *testing.T, s string) string {
				return edit(t, s, "    enabled: false\n", "    enabled: true\n")
			},
		},
		{
			name: "watch disable (already off: file untouched)",
			args: []string{"watch", "disable", "clipboard"},
			want: func(_ *testing.T, s string) string { return s },
		},
	}

	for _, scenario := range []struct {
		name       string
		viaOverlay bool
	}{
		// No "=" in these names: t.TempDir embeds them in the overlay
		// path, and kit parses a -c token holding "=" as key=value.
		{"target is the user file", false},
		{"target is the -c overlay file", true},
	} {
		for _, tc := range cases {
			t.Run(scenario.name+"/"+tc.name, func(t *testing.T) {
				e := newOwnLayerEnv(t, scenario.viaOverlay)
				if out, err := e.run(t, tc.args...); err != nil {
					t.Fatalf("%v: %v\n%s", tc.args, err, out)
				}
				e.assertTarget(t, tc.want(t, e.targetStart()))
			})
		}
	}
}

// TestConfigWriteCommandsCreateMissingTarget: a target file that does not
// exist yet is created holding only the intended key.
func TestConfigWriteCommandsCreateMissingTarget(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "profile create",
			args: []string{"profile", "create", "neo"},
			want: "profile:\n  profiles:\n    neo:\n      description: Profile for neo\n",
		},
		{
			name: "profile set (profile from another layer)",
			args: []string{"profile", "set", "projonly"},
			want: "profile:\n  default: projonly\n",
		},
		{
			name: "watch enable",
			args: []string{"watch", "enable", "clipboard"},
			want: "watch:\n  clipboard:\n    enabled: true\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newOwnLayerEnv(t, false)
			if err := os.Remove(e.user); err != nil {
				t.Fatal(err)
			}
			delete(e.before, e.user)
			if out, err := e.run(t, tc.args...); err != nil {
				t.Fatalf("%v: %v\n%s", tc.args, err, out)
			}
			e.assertTarget(t, tc.want)
			info, err := os.Stat(e.user)
			if err != nil {
				t.Fatal(err)
			}
			if perm := info.Mode().Perm(); perm != 0o600 {
				t.Errorf("created config mode = %o; want 600", perm)
			}
		})
	}
}

// TestConfigWriteCommandsRefuseEntriesFromOtherLayers: an entry that lives
// in another layer cannot be changed through the target file. The command
// fails, names the target, and writes nothing.
func TestConfigWriteCommandsRefuseEntriesFromOtherLayers(t *testing.T) {
	for _, args := range [][]string{
		{"profile", "rm", "--confirm=yes", "projonly"},
		{"profile", "schema", "add-type", "projonly", "task"},
		{"profile", "schema", "add-topic", "projonly", "ops"},
		{"profile", "schema", "add-rule", "projonly", "x", "task"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			e := newOwnLayerEnv(t, false)
			out, err := e.run(t, args...)
			if err == nil {
				t.Fatalf("%v succeeded; want an error naming the target file\n%s", args, out)
			}
			if !strings.Contains(err.Error(), e.target) {
				t.Errorf("error %q does not name the target file %s", err, e.target)
			}
			e.assertTarget(t, e.targetStart())
		})
	}
}

// TestSetupEditsOnlyWizardKeys: setup writes the wizard's keys into the
// user file and leaves every other key, comment and blank line alone.
func TestSetupEditsOnlyWizardKeys(t *testing.T) {
	e := newOwnLayerEnv(t, false)
	out, err := e.run(t, "setup", "--non-interactive")
	if err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	data := filepath.Join(e.home, ".local", "share", "ctxt")
	want := edit(t, e.targetStart(), "  path: "+e.db+"\n",
		"  path: "+filepath.Join(data, "db.sqlite")+"\n"+
			"  blob:\n    backend: local\n    local:\n      path: "+filepath.Join(data, "blobs")+"\n")
	want += "inbox:\n  pipeline: auto\n"
	e.assertTarget(t, want)
}

// TestSetupCreatesMissingUserFile: with no user file yet, setup creates
// one holding only the wizard's keys.
func TestSetupCreatesMissingUserFile(t *testing.T) {
	e := newOwnLayerEnv(t, false)
	if err := os.Remove(e.user); err != nil {
		t.Fatal(err)
	}
	delete(e.before, e.user)
	out, err := e.run(t, "setup", "--non-interactive")
	if err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	data := filepath.Join(e.home, ".local", "share", "ctxt")
	e.assertTarget(t, "storage:\n"+
		"  type: sqlite\n"+
		"  path: "+filepath.Join(data, "db.sqlite")+"\n"+
		"  blob:\n    backend: local\n    local:\n      path: "+filepath.Join(data, "blobs")+"\n"+
		"inbox:\n  pipeline: auto\n")
}

// TestProfileDefaultDropsContradictingInlineFlag: a profile's inline
// `default: true` is the other spelling of profile.default. Moving or
// clearing the default removes it from the target file, so the file still
// loads (two defaults are a load error) and a clear stays cleared.
func TestProfileDefaultDropsContradictingInlineFlag(t *testing.T) {
	const start = "profile:\n  profiles:\n    work:\n      default: true\n      description: Work\n    home:\n      description: Home\n"
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "set moves it",
			args: []string{"profile", "set", "home"},
			want: "profile:\n  profiles:\n    work:\n      description: Work\n    home:\n      description: Home\n  default: home\n",
		},
		{
			name: "unset clears it",
			args: []string{"profile", "unset", "work"},
			want: "profile:\n  profiles:\n    work:\n      description: Work\n    home:\n      description: Home\n  default: \"\"\n",
		},
		{
			name: "set of the flagged profile keeps it",
			args: []string{"profile", "set", "work"},
			want: start + "  default: work\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newOwnLayerEnv(t, false)
			writeFile(t, e.user, start)
			e.before[e.user] = []byte(start)
			if out, err := e.run(t, tc.args...); err != nil {
				t.Fatalf("%v: %v\n%s", tc.args, err, out)
			}
			e.assertTarget(t, tc.want)
			if out, err := e.run(t, "profile", "list"); err != nil {
				t.Fatalf("config no longer loads after %v: %v\n%s", tc.args, err, out)
			}
		})
	}
}

// TestRegistryDeleteRefusesEntryFromOtherLayer: a registry another config
// file declares cannot be dropped through the target file; the command
// fails before touching the cache, and no file changes.
func TestRegistryDeleteRefusesEntryFromOtherLayer(t *testing.T) {
	e := newOwnLayerEnv(t, false)
	project := "registries:\n  - name: gamma\n    url: https://gamma.example\n"
	writeFile(t, e.project, project)
	e.before[e.project] = []byte(project)

	out, err := e.run(t, "registry", "delete", "--confirm=yes", "gamma")
	if err == nil {
		t.Fatalf("registry delete gamma succeeded; want an error\n%s", out)
	}
	if !strings.Contains(err.Error(), e.target) {
		t.Errorf("error %q does not name the target file %s", err, e.target)
	}
	e.assertTarget(t, e.targetStart())
}
