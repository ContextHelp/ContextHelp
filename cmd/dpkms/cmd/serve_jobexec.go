package cmd

import (
	"fmt"
	"io"
	"os"

	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/jobs/podexec"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/podrun"
)

type jobExecDeps struct {
	detect      func() (string, error)
	runner      podrun.Runner
	projectSets func(bin, cfgFile string, extraPaths []string, key string) (string, bool, error)
	warn        io.Writer
}

var defaultJobExecDeps = jobExecDeps{
	detect:      podrun.Detect,
	projectSets: config.ProjectLayerSets,
	warn:        os.Stderr,
}

// prepareJobExecutor validates jobs.executor / jobs.pod.* before serve binds
// listeners or acquires locks, and returns an installer to attach the executor
// to WorkerPool. When jobs.executor is "local" (default), podrun.Detect is
// NEVER called and a nil installer is returned.
func prepareJobExecutor(c *config.Config, deps jobExecDeps) (func(pool *jobs.WorkerPool, pipes pipeline.Registry), error) {
	checkProject := deps.projectSets
	if checkProject == nil {
		checkProject = config.ProjectLayerSets
	}
	var extraPaths []string
	if root != nil {
		extraPaths, _, _ = root.ConfigArgs()
	}
	for _, key := range []string{"jobs.executor", "jobs.pod"} {
		if projPath, found, err := checkProject(binName, cfgFile, extraPaths, key); err != nil {
			return nil, err
		} else if found {
			e := output.UsageError(fmt.Sprintf("project config %q may not set %s", projPath, key))
			e.SuggestedFix = "move jobs.executor and jobs.pod.* to your user config"
			return nil, e
		}
	}

	if c == nil || c.Jobs.EffectiveExecutor() == config.JobExecutorLocal {
		return nil, nil
	}
	if c.Jobs.EffectiveExecutor() != config.JobExecutorPod {
		return nil, output.UsageError(fmt.Sprintf("unknown jobs.executor %q (want %s or %s)", c.Jobs.Executor, config.JobExecutorLocal, config.JobExecutorPod))
	}

	detect := deps.detect
	if detect == nil {
		detect = podrun.Detect
	}
	warn := deps.warn
	if warn == nil {
		warn = os.Stderr
	}

	binPath, err := detect()
	if err != nil {
		if c.Jobs.Pod.FallbackLocal {
			_, _ = fmt.Fprintf(warn, "warning: jobs.executor=pod requested but pod binary is unavailable (%v); falling back to local executor\n", err)
			return nil, nil
		}
		return nil, err
	}

	ct := c.Jobs.Pod.EffectiveTimeouts()
	client := podrun.NewClient(binPath, deps.runner, podrun.Timeouts{
		Probe:     ct.Probe,
		Query:     ct.Query,
		Exec:      ct.Exec,
		Lifecycle: ct.Lifecycle,
	})
	client.Warn = warn

	podExec := podexec.NewExecutor(client, podexec.Options{
		Provider:       c.Jobs.Pod.Provider,
		Image:          c.Jobs.Pod.Image,
		DpkmsBin:       c.Jobs.Pod.EffectiveDpkmsBin(),
		MaxInflight:    c.Jobs.Pod.EffectiveMaxInflight(),
		MaxPodsPerHour: c.Jobs.Pod.EffectiveMaxPodsPerHour(),
		MaxPodDuration: c.Jobs.Pod.EffectiveMaxPodDuration(),
		MaxHops:        c.Jobs.MaxHops,
		Warn:           warn,
	})
	topics := c.Jobs.Pod.EffectiveTopics()
	maxHops := c.Jobs.MaxHops

	return func(pool *jobs.WorkerPool, pipes pipeline.Registry) {
		localExec := &jobs.LocalExecutor{Pipelines: pipes, MaxHops: maxHops}
		pool.SetExecutor(podexec.NewRouter(localExec, podExec, topics))
	}, nil
}
