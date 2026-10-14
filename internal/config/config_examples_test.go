package config

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// commentedSetting matches a commented-out setting in a config/*.yaml
// template: '#' directly followed by the YAML line, its indent even and
// its content a key or a list item. A '#' followed by one space is prose.
var commentedSetting = regexp.MustCompile(`(?m)^#((?:  )*(?:- |[A-Za-z_][A-Za-z0-9_.-]*:).*)$`)

// placeholderToken is the one Validate error a template may carry once
// its settings are enabled: a static token still holding REPLACE_ME,
// which the operator must replace before serve starts.
var placeholderToken = regexp.MustCompile(`^server\.auth\.static\.tokens\[\d+\]\.token$`)

// The config/*.yaml files are the templates the docs tell readers to copy.
// Each must hold only keys Config declares and values Validate accepts,
// as copied and with every commented setting enabled: a key Load does not
// read, in a template, is a setting that silently does nothing.
func TestConfigExamples_ValidAgainstConfig(t *testing.T) {
	clearPortEnv(t)
	for _, b := range envBindings {
		t.Setenv(b.Env, "")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(EnvConfigPath, "")

	files, err := filepath.Glob(filepath.Join("..", "..", "config", "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, files)

	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			asCopied, err := os.ReadFile(f)
			require.NoError(t, err)
			enabled := commentedSetting.ReplaceAll(asCopied, []byte("$1"))

			for _, v := range []struct {
				name string
				data []byte
			}{{"as copied", asCopied}, {"settings enabled", enabled}} {
				dec := yaml.NewDecoder(bytes.NewReader(v.data))
				dec.KnownFields(true)
				var strict Config
				if err := dec.Decode(&strict); err != nil && !errors.Is(err, io.EOF) {
					t.Fatalf("%s (%s): %v", f, v.name, err)
				}

				path := filepath.Join(t.TempDir(), filepath.Base(f))
				require.NoError(t, os.WriteFile(path, v.data, 0o600))
				cfg, err := Load("dpkms", path)
				require.NoError(t, err, "%s (%s): load", f, v.name)
				for _, e := range Validate(cfg) {
					if v.name == "settings enabled" && placeholderToken.MatchString(e.Field) && strings.Contains(e.Message, "placeholder") {
						continue
					}
					assert.Fail(t, "validate", "%s (%s): %s", f, v.name, e)
				}
			}
		})
	}
}
