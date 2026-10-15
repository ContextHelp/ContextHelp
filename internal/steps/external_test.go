//go:build unix

package steps_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func draft() *storage.KnowledgeObject {
	return &storage.KnowledgeObject{
		ID:         "ko-1",
		RawContent: "mail jane@example.com",
		Source:     "cli",
		Pipeline:   "text.short",
		CreatedAt:  time.Date(2026, 10, 14, 9, 30, 0, 123, time.UTC),
		Vectors:    []storage.ObjectVector{{ModelID: "m"}},
	}
}

// emit is a step script that drains stdin and prints out.
func emit(out string) string {
	return "cat >/dev/null\nprintf '%s' '" + out + "'"
}

func TestExternalStepRewritesWritableFields(t *testing.T) {
	f := newFixture(t)
	f.install(t, "masker", skill("masker"), emit(`{"object":{"raw_content":"mail [email]","tags":[{"label":"pii"}]}}`))
	s := f.step(t, config.StepInsert{Step: "masker"})

	in := draft()
	out, err := s.Run(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, "mail [email]", out.RawContent)
	require.Len(t, out.Tags, 1)
	assert.Equal(t, "pii", out.Tags[0].Label)
	// Omitted fields keep their value; unserialized ones carry over.
	assert.Equal(t, "ko-1", out.ID)
	assert.Equal(t, "cli", out.Source)
	assert.Equal(t, in.Vectors, out.Vectors)
	assert.Equal(t, "mail jane@example.com", in.RawContent, "the input object is not mutated")
}

// A step may echo the whole object back: identical protected fields
// pass.
func TestExternalStepEchoPasses(t *testing.T) {
	f := newFixture(t)
	f.install(t, "echo", skill("echo"),
		`sed 's/jane@example.com/[email]/' | { read -r o; printf '{"object":%s}' "$o"; }`)
	out, err := f.step(t, config.StepInsert{Step: "echo"}).Run(context.Background(), draft())
	require.NoError(t, err)
	assert.Equal(t, "mail [email]", out.RawContent)
}

// Fail closed: every kind of failure fails the object; the error says
// which step and why, and never carries the object's content.
func TestExternalStepFailsClosed(t *testing.T) {
	cases := []struct {
		name      string
		script    string
		extra     []string
		permanent bool
		want      string
	}{
		{"non-zero exit", "cat >/dev/null; exit 3", nil, false, "exited with status 3"},
		{"reported error", emit(`{"object":null,"error":{"code":"NO_GOOD","message":"cannot parse"}}`) + "; exit 1", nil, true, "step reported NO_GOOD: cannot parse"},
		{"reported retryable error", emit(`{"error":{"code":"BUSY","message":"later","retryable":true}}`), nil, false, "step reported BUSY"},
		{"invalid json", emit(`not json`), nil, true, "invalid output"},
		{"empty output", "cat >/dev/null", nil, true, "invalid output: empty stdout"},
		{"neither object nor error", emit(`{"object":null}`), nil, true, "no object and no error"},
		{"unknown result field", emit(`{"object":{},"extra":1}`), nil, true, "invalid output"},
		{"id changed", emit(`{"object":{"id":"ko-2"}}`), nil, true, `field "id" is read-only`},
		{"source changed", emit(`{"object":{"source":"elsewhere"}}`), nil, true, `field "source" is read-only`},
		{"created_at changed", emit(`{"object":{"created_at":"2020-01-01T00:00:00Z"}}`), nil, true, `field "created_at" is read-only`},
		{"unknown object field", emit(`{"object":{"rawContent":"x"}}`), nil, true, `unknown object field "rawContent"`},
		{"timeout", "cat >/dev/null; sleep 5", []string{"resource_requirements:", "  timeout: 300ms"}, false, "timed out after 300ms"},
		{"output too large", "cat >/dev/null; head -c 4096 /dev/zero", []string{"resource_requirements:", "  max_output: 1KB"}, true, "output exceeds max_output"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.install(t, "flaky", skill("flaky", tc.extra...), tc.script)
			start := time.Now()
			out, err := f.step(t, config.StepInsert{Step: "flaky"}).Run(context.Background(), draft())
			require.Error(t, err)
			assert.Nil(t, out, "a failed step passes no object on")
			assert.Contains(t, err.Error(), tc.want)
			assert.Contains(t, err.Error(), "flaky")
			assert.NotContains(t, err.Error(), "jane@example.com", "content leaked into the error")
			assert.Equal(t, tc.permanent, pipeline.IsPermanent(err), "permanent: %v", err)
			assert.Less(t, time.Since(start), 4*time.Second)
		})
	}
}

// on_error: continue passes the object on unchanged, explicitly per step.
func TestExternalStepContinueOnError(t *testing.T) {
	f := newFixture(t)
	f.install(t, "flaky", skill("flaky"), "cat >/dev/null; exit 1")
	in := draft()
	out, err := f.step(t, config.StepInsert{Step: "flaky", OnError: config.StepOnErrorContinue}).Run(context.Background(), in)
	require.NoError(t, err)
	assert.Same(t, in, out)
}

// The run's environment is the protocol variables plus PATH, HOME,
// TMPDIR and LC_ALL: nothing of dpkms's own. The working directory is
// private and gone after the run; a descriptor dpkms holds open is not
// inherited.
func TestExternalStepEnvironmentIsolation(t *testing.T) {
	t.Setenv("DPKMS_TEST_SECRET", "leak-me")
	t.Setenv("OPENAI_API_KEY", "sk-leak-me")
	held, err := os.Open(filepath.Join(t.TempDir(), "."))
	require.NoError(t, err)
	defer held.Close()
	fd := held.Fd()

	f := newFixture(t)
	script := fmt.Sprintf(`cat >/dev/null
envs=$(env | cut -d= -f1 | sort | tr '\n' ' ')
if [ -e /dev/fd/%d ]; then fd=open; else fd=closed; fi
printf '{"object":{"metadata":{"env":"%%s","cwd":"%%s","home":"%%s","fd":"%%s","cfg":"%%s"}}}' "$envs" "$(pwd -P)" "$HOME" "$fd" "$(cat "$DPKMS_STEP_CONFIG_PATH")" | sed 's/"cfg":"{"mask":"x"}"/"cfg":"ok"/'`, fd)
	f.install(t, "probe", skill("probe"), script)
	s := f.step(t, config.StepInsert{Step: "probe", Config: map[string]any{"mask": "x"}})
	out, err := s.Run(context.Background(), draft())
	require.NoError(t, err)

	md := out.Metadata
	env := strings.Fields(md["env"].(string))
	assert.ElementsMatch(t, []string{
		"DPKMS_PIPELINE_NAME", "DPKMS_STEP_CONFIG_PATH", "DPKMS_STEP_NAME", "HOME", "LC_ALL", "PATH",
		"PWD", "SANDBOX_ENABLED", "SANDBOX_ISOLATION_LEVEL", "SANDBOX_NETWORK", "SANDBOX_TIMEOUT", "SHLVL", "TMPDIR", "_",
	}, filterShellVars(env))
	assert.Equal(t, "closed", md["fd"], "a descriptor dpkms holds leaked into the step")
	assert.Equal(t, "ok", md["cfg"], "step config not at DPKMS_STEP_CONFIG_PATH")
	cwd := md["cwd"].(string)
	home := md["home"].(string)
	assert.True(t, strings.HasPrefix(filepath.Base(cwd), "dpkms-step-"), cwd)
	assert.Equal(t, filepath.Base(cwd), filepath.Base(home), "HOME is the private working directory")
	_, statErr := os.Stat(cwd)
	assert.True(t, errors.Is(statErr, os.ErrNotExist), "working directory survived the run")
}

// filterShellVars keeps the names the protocol or the shell defines;
// shells differ in which of PWD, SHLVL and _ they export, so those are
// normalized to present.
func filterShellVars(env []string) []string {
	have := map[string]bool{}
	for _, e := range env {
		have[e] = true
	}
	for _, sh := range []string{"PWD", "SHLVL", "_"} {
		have[sh] = true
	}
	out := make([]string, 0, len(have))
	for k := range have {
		out = append(out, k)
	}
	return out
}

// An executable changed after install never runs.
func TestExternalStepRefusesTamperedExecutable(t *testing.T) {
	f := newFixture(t)
	rs := f.install(t, "masker", skill("masker"), emit(`{"object":{}}`))
	s := f.step(t, config.StepInsert{Step: "masker"})

	marker := filepath.Join(t.TempDir(), "ran")
	require.NoError(t, os.WriteFile(filepath.Join(rs.Path, "run.sh"),
		[]byte("#!/bin/sh\ntouch "+marker+"\nprintf '{\"object\":{}}'\n"), 0o700)) // #nosec G306 -- test step executable

	out, err := s.Run(context.Background(), draft())
	require.Error(t, err)
	assert.Nil(t, out)
	assert.True(t, pipeline.IsPermanent(err))
	assert.Contains(t, err.Error(), "changed since install")
	_, statErr := os.Stat(marker)
	assert.True(t, errors.Is(statErr, os.ErrNotExist), "tampered executable ran")
}

func TestCatalogStepConfigSchema(t *testing.T) {
	f := newFixture(t)
	f.install(t, "masker", skill("masker",
		"config_schema:",
		"  type: object",
		"  additionalProperties: false",
		"  properties:",
		"    mask: {type: string}",
	), emit(`{"object":{}}`))
	cat, err := steps.LoadCatalog(context.Background(), f.store, steps.DefaultRunners())
	require.NoError(t, err)
	_, err = cat.Step("text.short", config.StepInsert{Step: "masker", Config: map[string]any{"mask": "x"}})
	require.NoError(t, err)
	_, err = cat.Step("text.short", config.StepInsert{Step: "masker", Config: map[string]any{"mask": 3}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config does not match its config_schema")
	_, err = cat.Step("text.short", config.StepInsert{Step: "masker", Config: map[string]any{"other": "x"}})
	require.Error(t, err)
}

// The operator's timeout overrides SKILL.md's.
func TestCatalogTimeoutOverride(t *testing.T) {
	f := newFixture(t)
	f.install(t, "slow", skill("slow", "resource_requirements:", "  timeout: 1m"), "cat >/dev/null; sleep 5")
	start := time.Now()
	_, err := f.step(t, config.StepInsert{Step: "slow", Timeout: 200 * time.Millisecond}).Run(context.Background(), draft())
	require.ErrorIs(t, err, steps.ErrStepTimeout)
	assert.Less(t, time.Since(start), 3*time.Second)
}
