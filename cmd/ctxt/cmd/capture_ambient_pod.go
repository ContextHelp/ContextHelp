package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"
	"hop.top/kit/go/core/xdg"

	"github.com/ideacrafterslabs/ctxt/internal/ambient/podhost"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/podrun"
)

// ambientPodDeps holds injectable seams for testing the ambient runner
// decision without starting real processes or touching real PATH/pod state.
type ambientPodDeps struct {
	detect      func() (string, error)
	runner      podrun.Runner
	projectSets func(bin, cfgFile string, extraPaths []string, key string) (string, bool, error)
	localStart  func(cmd *cobra.Command, args []string) error
}

var defaultAmbientPodDeps = ambientPodDeps{
	detect:      podrun.Detect,
	projectSets: config.ProjectLayerSets,
}

// resolveAmbientDaemonRunner determines whether `ctxt capture --ambient`
// runs locally or on a pod, enforcing C1 (flag > config > default), C2
// (refuse daemon.* in project layer), and C4 (validation).
//
// When runner is "local", podrun.Detect is NEVER called (D2).
func resolveAmbientDaemonRunner(cmd *cobra.Command, c *config.Config, deps ambientPodDeps) (runnerMode, podName string, err error) {
	checkProject := deps.projectSets
	if checkProject == nil {
		checkProject = config.ProjectLayerSets
	}
	var extraPaths []string
	if root != nil {
		extraPaths, _, _ = root.ConfigArgs()
	}
	if projPath, found, perr := checkProject(binName, cfgFile, extraPaths, "daemon"); perr != nil {
		return "", "", perr
	} else if found {
		e := output.UsageError(fmt.Sprintf("project config %q may not set daemon.* keys", projPath))
		e.SuggestedFix = "move daemon.* to your user config, or pass --runner and --pod"
		return "", "", e
	}

	mode := config.DaemonRunnerLocal
	if c != nil {
		mode = c.Daemon.EffectiveRunner()
		podName = c.Daemon.Pod.Name
	}
	if cmd.Flags().Changed("runner") {
		mode, _ = cmd.Flags().GetString("runner")
	}
	if cmd.Flags().Changed("pod") {
		podName, _ = cmd.Flags().GetString("pod")
	}

	switch mode {
	case "", config.DaemonRunnerLocal:
		return config.DaemonRunnerLocal, "", nil
	case config.DaemonRunnerPod:
		if podName == "" {
			e := output.UsageError("daemon.runner is \"pod\" but no pod name is set")
			e.SuggestedFix = "set daemon.pod.name in user config or pass --pod <name>"
			return "", "", e
		}
		if err := podrun.ValidateToken("daemon.pod.name", podName); err != nil {
			return "", "", err
		}
		return config.DaemonRunnerPod, podName, nil
	default:
		e := output.UsageError(fmt.Sprintf("unknown --runner %q (want %s or %s)", mode, config.DaemonRunnerLocal, config.DaemonRunnerPod))
		e.SuggestedFix = "pass --runner local or --runner pod"
		return "", "", e
	}
}

func runAmbientPod(cmd *cobra.Command, args []string, deps ambientPodDeps) error {
	mode, podName, err := resolveAmbientDaemonRunner(cmd, cfg, deps)
	if err != nil {
		return err
	}
	if mode == config.DaemonRunnerLocal {
		if deps.localStart != nil {
			return deps.localStart(cmd, args)
		}
		return fmt.Errorf("ambient mode not yet implemented (Track 2)")
	}

	// Refuse --foreground in pod mode (v1).
	if fg, _ := cmd.Flags().GetBool("foreground"); fg {
		e := output.UsageError("--foreground is not supported with --runner pod")
		e.SuggestedFix = "omit --foreground; inspect logs with `ctxt capture --ambient tail --runner pod --pod " + podName + "`"
		return e
	}

	detect := deps.detect
	if detect == nil {
		detect = podrun.Detect
	}
	binPath, err := detect()
	if err != nil {
		return err
	}

	var to podrun.Timeouts
	var podCfg config.DaemonPodConfig
	if cfg != nil {
		podCfg = cfg.Daemon.Pod
		ct := podCfg.EffectiveTimeouts()
		to = podrun.Timeouts{
			Probe:     ct.Probe,
			Query:     ct.Query,
			Exec:      ct.Exec,
			Lifecycle: ct.Lifecycle,
		}
	}
	client := podrun.NewClient(binPath, deps.runner, to)
	client.Warn = cmd.ErrOrStderr()

	stateDir, err := ambientPodStateDir()
	if err != nil {
		return err
	}
	host := podhost.NewHost(client, stateDir)
	host.Warn = cmd.ErrOrStderr()

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	verb := ""
	if len(args) > 0 {
		verb = args[0]
	}

	switch verb {
	case "":
		return runAmbientPodStart(ctx, cmd, host, podName, podCfg)
	case "status":
		st, alive, err := host.Status(ctx, podName)
		if err != nil {
			return err
		}
		if isJSONOutput() {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(st)
		}
		if !alive {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "ctxd is not running (pod %s)\n", podName)
			return nil
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "ctxd is running on pod %s\n", podName)
		return nil
	case "stop":
		stopPod, _ := cmd.Flags().GetBool("stop-pod")
		if err := host.Stop(ctx, podName, stopPod); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Stopped ctxd on pod %s\n", podName)
		return nil
	case "sources":
		st, alive, err := host.Status(ctx, podName)
		if err != nil {
			return err
		}
		if !alive {
			return output.NotFoundError(fmt.Sprintf("ctxd is not running on pod %q", podName))
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(st)
	case "tail":
		return host.Tail(ctx, podName, cmd.OutOrStdout())
	case "buffer":
		if len(args) != 2 || (args[1] != "flush" && args[1] != "purge") {
			return output.UsageError("capture --ambient buffer requires flush or purge")
		}
		res, err := host.BufferAction(ctx, podName, args[1])
		if err != nil {
			return err
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	default:
		e := output.UsageError(fmt.Sprintf("unknown capture --ambient verb %q", verb))
		e.SuggestedFix = "use one of: status, stop, sources, tail, buffer flush, buffer purge (or no verb to start ctxd)"
		return e
	}
}

func runAmbientPodStart(ctx context.Context, cmd *cobra.Command, host *podhost.Host, podName string, podCfg config.DaemonPodConfig) error {
	var resolvedEndpoint, serverToken string
	if podCfg.ServerURL == "" && cfg != nil {
		if ep, err := resolveEndpoint(cmd); err == nil {
			resolvedEndpoint = ep.URL
			serverToken = ep.Token
		}
	}

	inputs, _ := cmd.Flags().GetStringSlice("input")
	skips, _ := cmd.Flags().GetStringSlice("skip")
	res, err := host.Start(ctx, podhost.StartOpts{
		PodName:          podName,
		Autostart:        podCfg.EffectiveAutostart(),
		CtxdBin:          podCfg.EffectiveCtxdBin(),
		ServerURL:        podCfg.ServerURL,
		ResolvedEndpoint: resolvedEndpoint,
		ServerToken:      serverToken,
		Profile:          activeProfileName(cmd),
		Inputs:           inputs,
		Skips:            skips,
	})
	if err != nil {
		return err
	}
	if isJSONOutput() {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "ctxd started on pod %s (provider: %s, pid %d)\n", res.Pod, res.Provider, res.Run.PID)
	return nil
}

func ambientPodStateDir() (string, error) {
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "ctxt", "ambient"), nil
	}
	dir, err := xdg.StateDir("ctxt")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ambient"), nil
}
