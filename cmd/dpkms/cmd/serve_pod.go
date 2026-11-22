package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"
	"hop.top/kit/go/core/xdg"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmspod"
	"github.com/ideacrafterslabs/ctxt/internal/podrun"
	"github.com/ideacrafterslabs/ctxt/internal/server/stack"
)

type dpkmsPodDeps struct {
	detect      func() (string, error)
	runner      podrun.Runner
	projectSets func(bin, cfgFile string, extraPaths []string, key string) (string, bool, error)
	stateDir    func() (string, error)
}

var defaultDpkmsPodDeps = dpkmsPodDeps{
	detect:      podrun.Detect,
	projectSets: config.ProjectLayerSets,
	stateDir:    dpkmsPodStateDir,
}

// resolveDpkmsDaemonRunner is the pure runner-selection seam that runs before
// acquireListeners, storage lock, or local daemonize. When runner resolves to
// "local", podrun.Detect is NEVER called.
func resolveDpkmsDaemonRunner(cmd *cobra.Command, c *config.Config, deps dpkmsPodDeps) (mode, podName string, err error) {
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

	mode = config.DaemonRunnerLocal
	if c != nil {
		mode = c.Daemon.EffectiveRunner()
		podName = c.Daemon.Pod.Name
	}
	if f := cmd.Flags().Lookup("runner"); f != nil && f.Changed {
		mode, _ = cmd.Flags().GetString("runner")
	}
	if f := cmd.Flags().Lookup("pod"); f != nil && f.Changed {
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

func newDpkmsPodHost(cmd *cobra.Command, deps dpkmsPodDeps) (*dpkmspod.Host, config.DaemonPodConfig, error) {
	detect := deps.detect
	if detect == nil {
		detect = podrun.Detect
	}
	binPath, err := detect()
	if err != nil {
		return nil, config.DaemonPodConfig{}, err
	}

	var podCfg config.DaemonPodConfig
	var to podrun.Timeouts
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

	sdFn := deps.stateDir
	if sdFn == nil {
		sdFn = dpkmsPodStateDir
	}
	stateDir, err := sdFn()
	if err != nil {
		return nil, config.DaemonPodConfig{}, err
	}
	host := dpkmspod.NewHost(client, stateDir)
	host.Warn = cmd.ErrOrStderr()
	return host, podCfg, nil
}

func maybeRunServePod(cmd *cobra.Command, _ []string, deps dpkmsPodDeps) (bool, error) {
	mode, podName, err := resolveDpkmsDaemonRunner(cmd, cfg, deps)
	if err != nil {
		return true, err
	}
	if mode == config.DaemonRunnerLocal {
		return false, nil
	}

	host, podCfg, err := newDpkmsPodHost(cmd, deps)
	if err != nil {
		return true, err
	}

	public, _ := cmd.Flags().GetBool("public")
	access := config.AccessPrivate
	var allowedHosts []string
	var authCfg config.AuthConfig
	if cfg != nil {
		if resolved, aerr := stack.ResolveInboundAuth(cfg, public); aerr == nil {
			access = resolved
		} else {
			return true, aerr
		}
		allowedHosts = cfg.Server.AllowedHosts
		authCfg = cfg.Server.Auth
	}

	instanceName, _ := cmd.Flags().GetString("name")
	profile, _ := cmd.Flags().GetString("profile")
	if profile == "" && cfg != nil {
		profile = cfg.Profile.Default
	}
	dpkmsPort := podCfg.EffectiveDpkmsPort()
	if cmd.Flags().Changed("port") {
		if p, _ := cmd.Flags().GetInt("port"); p > 0 {
			dpkmsPort = p
		}
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	res, err := host.Serve(ctx, dpkmspod.ServeOpts{
		PodName:      podName,
		Autostart:    podCfg.EffectiveAutostart(),
		DpkmsBin:     podCfg.EffectiveDpkmsBin(),
		InstanceName: instanceName,
		DataDir:      podCfg.DataDir,
		DpkmsPort:    dpkmsPort,
		LocalPort:    podCfg.LocalPort,
		Profile:      profile,
		Access:       access,
		AllowedHosts: allowedHosts,
		Auth:         authCfg,
	})
	if err != nil {
		return true, err
	}

	daemonMode, _ := cmd.Flags().GetBool("daemon")
	if isJSONOutput() {
		_ = outputJSON(cmd.OutOrStdout(), res)
	} else {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "dpkms running on pod %s (provider: %s)\n", res.Pod, res.Provider)
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "URL: %s\n", res.URL)
	}
	if daemonMode {
		// Detached forward helper stays running; PID recorded in local record.
		return true, nil
	}

	// Attached mode: wait for interrupt or unexpected forward exit.
	stopOnExit, _ := cmd.Flags().GetBool("stop-on-exit")
	return true, waitAttachedForward(ctx, cmd, host, res, podCfg, stopOnExit)
}

func waitAttachedForward(ctx context.Context, cmd *cobra.Command, host *dpkmspod.Host, res *dpkmspod.ServeResult, podCfg config.DaemonPodConfig, stopOnExit bool) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	fwdDone := make(chan error, 1)
	go func() {
		fwdDone <- res.ForwardProc.Wait()
	}()

	select {
	case err := <-fwdDone:
		return &output.Error{
			Code:         output.CodeTransient,
			Message:      fmt.Sprintf("forward to dpkms on pod %q exited unexpectedly: %v (remote dpkms is still running)", res.Pod, err),
			SuggestedFix: fmt.Sprintf("re-attach or stop with `dpkms shutdown --runner pod --pod %s`", res.Pod),
			ExitCode:     6,
		}
	case <-sigCh:
	case <-ctx.Done():
	}

	_ = res.ForwardProc.Close()
	if stopOnExit {
		sdCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		_, err := host.Shutdown(sdCtx, dpkmspod.ShutdownOpts{
			PodName:   res.Pod,
			DpkmsBin:  podCfg.EffectiveDpkmsBin(),
			DataDir:   res.DataDir,
			DpkmsPort: res.DpkmsPort,
		})
		return err
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Forward closed; dpkms is still running on pod %s. Stop it with: dpkms shutdown --runner pod --pod %s\n", res.Pod, res.Pod)
	return nil
}

func maybeRunPSPod(cmd *cobra.Command, deps dpkmsPodDeps) (bool, error) {
	mode, podName, err := resolveDpkmsDaemonRunner(cmd, cfg, deps)
	if err != nil {
		return true, err
	}
	if mode == config.DaemonRunnerLocal {
		return false, nil
	}

	host, podCfg, err := newDpkmsPodHost(cmd, deps)
	if err != nil {
		return true, err
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	res, err := host.PS(ctx, dpkmspod.PSOpts{
		PodName:  podName,
		DpkmsBin: podCfg.EffectiveDpkmsBin(),
		DataDir:  podCfg.DataDir,
	})
	if err != nil {
		return true, err
	}

	if isJSONOutput() {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return true, enc.Encode(map[string]any{
			"instances": res.Instances,
			"pod":       res.Pod,
			"status":    res.PodStatus,
		})
	}
	if len(res.Instances) == 0 {
		switch {
		case res.NotRunningReason != "":
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "No running dpkms instances (%s).\n", res.NotRunningReason)
		case res.LastLaunched != nil:
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "No running dpkms instances on pod %s (last launched %s).\n", res.Pod, res.LastLaunched.Format(time.RFC3339))
		default:
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "No running dpkms instances on pod %s.\n", res.Pod)
		}
		return true, nil
	}
	for _, row := range res.Instances {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\tpid=%d\tport=%d\thost=%s\thealth=%s\n", row.Name, row.PID, row.Port, row.Host, row.Health)
	}
	return true, nil
}

func maybeRunShutdownPod(cmd *cobra.Command, deps dpkmsPodDeps) (bool, error) {
	mode, podName, err := resolveDpkmsDaemonRunner(cmd, cfg, deps)
	if err != nil {
		return true, err
	}
	if mode == config.DaemonRunnerLocal {
		return false, nil
	}

	host, podCfg, err := newDpkmsPodHost(cmd, deps)
	if err != nil {
		return true, err
	}
	dpkmsPort := podCfg.EffectiveDpkmsPort()
	if cmd.Flags().Changed("port") {
		if p, _ := cmd.Flags().GetInt("port"); p > 0 {
			dpkmsPort = p
		}
	}
	stopPod, _ := cmd.Flags().GetBool("stop-pod")

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	res, err := host.Shutdown(ctx, dpkmspod.ShutdownOpts{
		PodName:   podName,
		DpkmsBin:  podCfg.EffectiveDpkmsBin(),
		DataDir:   podCfg.DataDir,
		DpkmsPort: dpkmsPort,
		StopPod:   stopPod,
	})
	if err != nil {
		return true, err
	}
	if isJSONOutput() {
		return true, outputJSON(cmd.OutOrStdout(), res)
	}
	if res.AlreadyDown {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "dpkms is not running (pod %s is %s)\n", res.Pod, res.PodStatus)
		return true, nil
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Stopped dpkms on pod %s (port %d)\n", res.Pod, res.DpkmsPort)
	return true, nil
}

func dpkmsPodStateDir() (string, error) {
	if s := os.Getenv("XDG_STATE_HOME"); s != "" {
		return filepath.Join(s, "ctxt", "dpkms"), nil
	}
	dir, err := xdg.StateDir("ctxt")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "dpkms"), nil
}
