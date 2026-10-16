package steps_test

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// fakeDocker records docker CLI calls and answers them with run.
type fakeDocker struct {
	mu    sync.Mutex
	calls [][]string
	envs  [][]string
	stdin []byte
	run   func(ctx context.Context, stdout io.Writer) error
}

func (d *fakeDocker) command(ctx context.Context, name string, args, env []string, stdin io.Reader, stdout, _ io.Writer) error {
	d.mu.Lock()
	d.calls = append(d.calls, append([]string{name}, args...))
	d.envs = append(d.envs, env)
	d.mu.Unlock()
	if args[0] != "run" {
		return nil
	}
	d.stdin, _ = io.ReadAll(stdin)
	return d.run(ctx, stdout)
}

func (d *fakeDocker) runner() steps.ContainerRunner {
	return steps.ContainerRunner{
		Command:  d.command,
		LookPath: func(string) (string, error) { return "/usr/bin/docker", nil },
	}
}

func argValue(args []string, flag string) []string {
	var out []string
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			out = append(out, args[i+1])
		}
	}
	return out
}

// A step from a sandboxed registry runs in a container with no network
// (even when it declares one), read-only, without capabilities, as an
// unprivileged user, with its limits, and sees only protocol variables.
func TestContainerIsolationArgs(t *testing.T) {
	t.Setenv("DPKMS_TEST_SECRET", "leak-me")
	f := newFixture(t)
	f.install(t, "boxed", skill("boxed",
		"container_image: alpine:3.20",
		"network: true",
		"resource_requirements:",
		"  max_memory: 128MB",
		"  max_cpu: 50%",
	), "true")
	// Mark it as a sandboxed registry's step.
	rs, err := f.store.Steps().Get(context.Background(), "boxed")
	require.NoError(t, err)
	rs.Metadata.Execution.TrustLevel = string(config.RegistryTrustLevelSandboxed)
	require.NoError(t, f.store.Steps().Update(context.Background(), rs))

	docker := &fakeDocker{run: func(_ context.Context, stdout io.Writer) error {
		_, err := io.WriteString(stdout, `{"object":{"raw_content":"boxed"}}`)
		return err
	}}
	procRan := false
	cat, err := steps.LoadCatalog(context.Background(), f.store, steps.Runners{
		Process:   runnerFunc(func() { procRan = true }),
		Container: docker.runner(),
	})
	require.NoError(t, err)
	s, err := cat.Step("text.short", config.StepInsert{Step: "boxed"})
	require.NoError(t, err)
	assert.Equal(t, storage.StepIsolationContainer, s.(*steps.ExternalStep).Isolation())

	out, err := s.Run(context.Background(), draft())
	require.NoError(t, err)
	assert.Equal(t, "boxed", out.RawContent)
	assert.False(t, procRan, "a sandboxed registry's step ran unsandboxed")

	require.Len(t, docker.calls, 1)
	args := docker.calls[0][1:]
	assert.Equal(t, []string{"none"}, argValue(args, "--network"))
	assert.Contains(t, args, "--read-only")
	assert.Equal(t, []string{"ALL"}, argValue(args, "--cap-drop"))
	assert.Equal(t, []string{"no-new-privileges"}, argValue(args, "--security-opt"))
	assert.Equal(t, []string{"65534:65534"}, argValue(args, "--user"))
	assert.Equal(t, []string{"128000000b"}, argValue(args, "--memory"))
	assert.Equal(t, []string{"0.5"}, argValue(args, "--cpus"))
	assert.Equal(t, []string{"/step/run.sh"}, argValue(args, "--entrypoint"))
	assert.Equal(t, "alpine:3.20", args[len(args)-1])
	assert.Equal(t, []string{rs.Path + ":/step:ro"}, argValue(args, "-v")[:1])

	var names []string
	for _, e := range argValue(args, "-e") {
		k, _, _ := strings.Cut(e, "=")
		names = append(names, k)
	}
	slices.Sort(names)
	assert.Equal(t, []string{
		"DPKMS_PIPELINE_NAME", "DPKMS_STEP_CONFIG_PATH", "DPKMS_STEP_NAME", "HOME",
		"SANDBOX_ENABLED", "SANDBOX_ISOLATION_LEVEL", "SANDBOX_MAX_CPU", "SANDBOX_MAX_MEMORY",
		"SANDBOX_NETWORK", "SANDBOX_TIMEOUT", "TMPDIR",
	}, names)
	assert.Contains(t, argValue(args, "-e"), "SANDBOX_NETWORK=false")
	for _, env := range docker.envs {
		for _, e := range env {
			assert.NotContains(t, e, "leak-me", "dpkms env passed to the docker CLI")
		}
	}
	assert.Contains(t, string(docker.stdin), `"id":"ko-1"`)
}

// A container that outlives its timeout is removed, and the run fails.
func TestContainerTimeoutRemovesContainer(t *testing.T) {
	docker := &fakeDocker{run: func(ctx context.Context, _ io.Writer) error {
		<-ctx.Done()
		return &exec.ExitError{}
	}}
	_, err := docker.runner().Run(context.Background(), steps.Invocation{
		StepDir: t.TempDir(), Executable: "run.sh", Image: "alpine:3.20",
		Timeout: 50 * time.Millisecond, WorkRoot: t.TempDir(),
	})
	require.ErrorIs(t, err, steps.ErrStepTimeout)
	require.Len(t, docker.calls, 2)
	name := argValue(docker.calls[0][1:], "--name")[0]
	assert.Equal(t, []string{"/usr/bin/docker", "rm", "-f", name}, docker.calls[1])
}

func TestContainerNeedsDocker(t *testing.T) {
	r := steps.ContainerRunner{LookPath: func(string) (string, error) { return "", errors.New("not found") }}
	_, err := r.Run(context.Background(), steps.Invocation{Image: "alpine:3.20"})
	require.ErrorIs(t, err, steps.ErrIsolationUnavailable)
}

// runnerFunc is a Runner that only records that it ran.
type runnerFunc func()

func (f runnerFunc) Run(context.Context, steps.Invocation) (steps.Result, error) {
	f()
	return steps.Result{}, errors.New("unexpected run")
}

func TestParseSkill(t *testing.T) {
	m, err := steps.ParseSkill([]byte(skill("masker", "minimum_isolation: process", "resource_requirements:", "  timeout: 5s")))
	require.NoError(t, err)
	assert.Equal(t, "masker", m.Name)
	assert.Equal(t, "run.sh", m.Execution.Executable)

	for _, bad := range []string{
		"no frontmatter",
		skill("x", "minimum_isolation: vm"),
		skill("x", "resource_requirements:", "  timeout: soon"),
		skill("x", "resource_requirements:", "  max_memory: lots"),
		skill("x", "config_schema:", "  type: 7"),
		strings.Replace(skill("x"), "type: external", "type: builtin", 1),
	} {
		_, err := steps.ParseSkill([]byte(bad))
		assert.ErrorIs(t, err, steps.ErrInvalidStep, bad)
	}
}
