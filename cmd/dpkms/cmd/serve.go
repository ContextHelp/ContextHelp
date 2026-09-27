package cmd

import (
	"context"
	"database/sql"
	"fmt"
	gohttp "net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/ideacrafterslabs/ctxt/internal/browser"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/federation"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
	"github.com/ideacrafterslabs/ctxt/internal/remind"
	grpcserver "github.com/ideacrafterslabs/ctxt/internal/server/grpc"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	"github.com/ideacrafterslabs/ctxt/internal/server/stack"
	wsserver "github.com/ideacrafterslabs/ctxt/internal/server/ws"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	"github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"

	kitbus "hop.top/kit/go/runtime/bus"
)

var serveCmd = &cobra.Command{
	Use:     "serve",
	Aliases: []string{"start"},
	Short:   "Start background worker and API server",
	Long: `Start the dPKMS server which includes:
  - Background job worker for processing ingestion pipelines
  - REST API server for HTTP access
  - gRPC API server for high-performance access

The worker processes jobs enqueued by 'ctxt analyze' and executes
the configured pipelines to create knowledge objects.

Examples:
  # Start with default settings
  dpkms serve

  # Start as a background daemon
  dpkms serve --daemon

  # Start with custom ports
  dpkms serve --port 8080 --grpc-port 9090

  # Start with more workers
  dpkms serve --workers 8

  # Allow remote connections
  dpkms serve --public

  # Use specific profile
  dpkms serve --profile founder

  # Named instance (required when running multiple instances)
  dpkms serve --name work
  dpkms serve --name personal --port 8081`,
	RunE: runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)

	// Long-running session-bound daemon. Kit refuses --dry-run on this
	// tier with a specific "no batch boundary" diagnostic.
	cliconv.WithSideEffect(serveCmd, cliconv.SideEffectInteractive)

	// Server flags
	serveCmd.Flags().String("name", "", "instance name (URI-safe slug, e.g. 'work'); defaults to DB basename")
	serveCmd.Flags().Int("port", 8080, "HTTP port")
	serveCmd.Flags().Int("grpc-port", 9090, "gRPC port")
	serveCmd.Flags().Int("workers", 4, "number of worker threads")
	serveCmd.Flags().Bool("public", false, "allow remote connections")
	serveCmd.Flags().String("profile", "", "default focus profile")
	serveCmd.Flags().String("steps-path", "", "path to external steps directory")
	serveCmd.Flags().Bool("dev", false, "enable CORS for Vite dev server (http://localhost:5173)")
	serveCmd.Flags().Duration("reminder-interval", time.Minute, "how often to check for due reminders")
	serveCmd.Flags().Bool("daemon", false, "run as background daemon (detach from terminal)")

	// Bind flags to viper
	viper.BindPFlag("server.port", serveCmd.Flags().Lookup("port"))
	viper.BindPFlag("server.grpc_port", serveCmd.Flags().Lookup("grpc-port"))
	viper.BindPFlag("server.workers", serveCmd.Flags().Lookup("workers"))
	viper.BindPFlag("server.public", serveCmd.Flags().Lookup("public"))
	viper.BindPFlag("profile.default", serveCmd.Flags().Lookup("profile"))
	viper.BindPFlag("steps.path", serveCmd.Flags().Lookup("steps-path"))
	viper.BindPFlag("server.dev", serveCmd.Flags().Lookup("dev"))
	viper.BindPFlag("server.reminder_interval", serveCmd.Flags().Lookup("reminder-interval"))
}

func runServe(cmd *cobra.Command, args []string) error {
	// --daemon: re-exec self in background, detached from terminal.
	if daemon, _ := cmd.Flags().GetBool("daemon"); daemon {
		return daemonize(cmd)
	}

	// Resolve instance name: flag > derive from DB basename.
	instanceName, err := resolveInstanceName(cmd)
	if err != nil {
		return err
	}

	// Resolve default profile: flag > config default > inline bool > interactive.
	if err := resolveDefaultProfile(cmd); err != nil {
		return err
	}

	port := viper.GetInt("server.port")
	grpcPort := viper.GetInt("server.grpc_port")
	workers := viper.GetInt("server.workers")
	public := viper.GetBool("server.public")
	reminderInterval := viper.GetDuration("server.reminder_interval")

	// Resolve the access class and inbound auth provider before any
	// resource is initialized: a protected/public instance with no
	// credentials refuses to start rather than exposing an open
	// listener.
	access, authProvider, err := stack.ResolveInboundAuth(cfg, public)
	if err != nil {
		return err
	}
	if cfg.Server.Access == "" && (public || cfg.Server.Public) {
		fmt.Println("note: --public/server.public is deprecated shorthand for server.access: public; set server.access explicitly")
	}
	if access != config.AccessPrivate {
		fmt.Printf("Inbound auth: %s provider (%s access)\n", authProvider.Name(), access)
	}

	// 1. Init storage.
	storageType := cfg.Storage.Type
	if storageType == "" {
		storageType = "sqlite"
	}
	storagePath := cfg.Storage.Path
	if storagePath == "" {
		storagePath = "dpkms.db"
	}

	driver, err := storageutil.NewDriver(storageType, storagePath)
	if err != nil {
		return fmt.Errorf("init storage: %w", err)
	}
	if err := driver.Init(context.Background()); err != nil {
		return fmt.Errorf("init storage: %w", err)
	}
	fmt.Println("Storage initialized")

	// 2. Browser automation daemon (optional).
	var browserMgr *browser.Manager
	var browserClient *browser.Client
	if cfg.Browser.Enabled {
		browserMgr = browser.NewManager(cfg.Browser)
		if err := browserMgr.Start(context.Background()); err != nil {
			return fmt.Errorf("browser daemon: %w", err)
		}
		defer func() {
			if err := browserMgr.Stop(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: browser daemon stop: %v\n", err)
			}
		}()
		browserClient = browserMgr.Client()
		fmt.Printf("IBR browser daemon on port %d\n", browserMgr.Port())
	}

	// 3. Cross-process event bus hub. The policy engine subscribes to
	// it, so it exists before the stack. Remote apps (aps, tlc) connect
	// to /ws/bus to share events.
	auth, ok := kitbus.AuthFromEnv("DPKMS_BUS_TOKEN", "BUS_TOKEN")
	if !ok {
		return fmt.Errorf("bus auth: set BUS_TOKEN or DPKMS_BUS_TOKEN env var")
	}
	hubBus := kitbus.New()
	hubNet := kitbus.NewNetworkAdapter(hubBus,
		kitbus.WithAuth(auth),
	)
	defer func() {
		_ = hubNet.Close()
		_ = hubBus.Close(context.Background())
	}()

	// 4. Get steps path.
	stepsPath := viper.GetString("steps.path")
	if stepsPath == "" {
		homeDir, _ := os.UserHomeDir()
		stepsPath = homeDir + "/.config/contexthelp/steps"
	}

	// 5. Upgrade-state manager (ADR-070 §5). Its in-memory state feeds
	// /healthz and the upgrade header on /api/v1 responses, which the CLI
	// banners read. It still writes the shadow file next to the pidfiles,
	// though nothing reads it any more; RunDir errors just disable it.
	var upgradeMgr *upgrade.Manager
	if runDir, runDirErr := config.RunDir(); runDirErr == nil {
		upgradeMgr = upgrade.NewManager(filepath.Join(runDir, "upgrade-state.json"))
	} else {
		upgradeMgr = upgrade.NewManager("")
	}

	// /healthz reports the binary's compiled version + serve start time.
	healthProbes := httpserver.HealthzProbes{
		Version: version,
		Started: time.Now(),
		Upgrade: func(_ context.Context) *httpserver.UpgradeSnapshot {
			return httpserver.NewUpgradeSnapshot(upgradeMgr.Snapshot())
		},
	}

	// 5a. Determine bind address. Only private instances stay on
	// loopback; protected/public bind all interfaces (and are already
	// guaranteed to have inbound auth configured above).
	bind := "127.0.0.1"
	if access != config.AccessPrivate {
		bind = "0.0.0.0"
	}

	// 5b. Init federation worker set. Cycle detection runs here; an
	// invalid topology fails serve before any port is bound (US-0318 AC).
	fedSet, err := federation.New(*cfg, driver)
	if err != nil {
		return fmt.Errorf("federation: %w", err)
	}
	if n := fedSet.Len(); n > 0 {
		fmt.Printf("Federation: %d async target(s) configured\n", n)
	}

	// 5c. Bind every listener once, before the stack (the Host
	// allowlist names the port actually bound), the pidfile or any
	// background goroutine, and hand each bound listener to its server.
	// Nothing probes a port and re-binds it later: two instances started
	// together can no longer both be told a port is free. Ports the
	// operator set (--port, --grpc-port) are bound exactly or serve
	// fails; built-in defaults fall back to an OS-assigned port. The
	// deferred close releases every port on an early return; after a
	// server has taken its listener the extra Close is a no-op.
	lns, err := acquireListeners(os.Stderr, []listenSpec{
		{name: "HTTP", bind: bind, port: port, explicit: cmd.Flags().Changed("port")},
		{name: "gRPC", bind: bind, port: grpcPort, explicit: cmd.Flags().Changed("grpc-port")},
		{name: "cookie bridge", bind: "127.0.0.1", port: wsserver.DefaultCookieBridgePort},
	})
	if err != nil {
		return err
	}
	defer closeListeners(lns)
	httpLn, grpcLn, cookieLn := lns[0], lns[1], lns[2]
	port, grpcPort = listenerPort(httpLn), listenerPort(grpcLn)
	cookieBridgePort := listenerPort(cookieLn)
	addr := httpLn.Addr().String()
	grpcBind := grpcLn.Addr().String()
	cookieBridgeAddr := cookieLn.Addr().String()
	// The bridge binds loopback whatever the access class, so it gets the
	// private rule: loopback names on the port it actually bound, nothing
	// else.
	cookieBridgeHosts, err := stack.HostAllowlist(config.AccessPrivate, cookieBridgePort, nil)
	if err != nil {
		return fmt.Errorf("cookie bridge hosts: %w", err)
	}

	// 6. Request-serving stack: queue, pipeline runtime, search engine,
	// event bus, policy engine, service, watcher manager, security
	// emitter, entitlement gate and HTTP router. The in-process test
	// server builds the same stack.
	embResolver := newEmbeddingResolver()
	st, err := stack.Build(stack.Inputs{
		Config:     cfg,
		Driver:     driver,
		Access:     access,
		Auth:       authProvider,
		PolicyBus:  hubBus,
		Embeddings: embResolver,
		Browser:    browserClient,
		StepsPath:  stepsPath,
		ConfigPath: config.GetConfigPath(binName),
		Probes:     healthProbes,
		DevCORS:    viper.GetBool("server.dev"),
		HTTPPort:   port,
		Warnings:   os.Stderr,
		Notes:      os.Stdout,
	})
	if err != nil {
		return err
	}
	defer st.Close()
	queue, pipes, svc, watchMgr := st.Queue, st.Pipelines, st.Service, st.Watcher
	fmt.Println("Job queue initialized")
	fmt.Println("Pipeline runtime initialized (with overrides)")
	reportEmbeddingProvider(os.Stdout, embResolver)

	// 6a. ADR-070 §3: verify the FTS index signature on startup, on either
	// backend. A mismatch logs a warning and emits a bus event; once the
	// worker pool is up, it schedules the re-projection job (10e), which
	// stamps the signature when done. The daemon proceeds meanwhile.
	var (
		sigDB      *sql.DB
		sigDialect indexsig.Dialect
		ftsVerify  *indexsig.VerifyResult
	)
	switch drv := driver.(type) {
	case *sqlite.Driver:
		sigDB, sigDialect = drv.DB(), indexsig.DialectSQLite
	case *postgres.Driver:
		sigDB, sigDialect = drv.DB(), indexsig.DialectPostgres
	}
	if sigDB != nil {
		if res, err := indexsig.VerifyFTS(context.Background(), sigDB, sigDialect); err != nil {
			fmt.Fprintf(os.Stderr, "warning: fts signature verify: %v\n", err)
		} else if !res.Match {
			ftsVerify = res
			if res.FirstBoot {
				fmt.Printf("FTS signature: first-boot stamp %s (inputs: %s)\n",
					shortHash(res.NewHash), res.InputsSummary)
			} else {
				fmt.Fprintf(os.Stderr,
					"warning: FTS signature mismatch: old=%s new=%s inputs=%s — reindex_auto: re-projecting stored objects\n",
					shortHash(res.OldHash), shortHash(res.NewHash), res.InputsSummary,
				)
			}
			ev, err := events.NewEvent(
				"dpkms.serve",
				string(events.TopicDpkmsUpgradeSignatureMismatch),
				events.UpgradeSignatureMismatchPayload{
					SignatureID:   res.SignatureID,
					OldHash:       res.OldHash,
					NewHash:       res.NewHash,
					InputsSummary: res.InputsSummary,
				},
			)
			if err == nil {
				_ = st.Bus.Publish(context.Background(), ev)
			}
		}
	}

	// Serve-time federation-credential validation: on non-private
	// instances the push route is gated per federation.token, never by
	// a principal token alone; without one the route refuses requests.
	if access != config.AccessPrivate && cfg.Federation.Token == "" {
		fmt.Printf("Federation push: refused — no federation.token configured (mandatory on %s instances)\n", access)
	}

	// 10. Init worker pool.
	pool := jobs.NewWorkerPool(queue, pipes, driver, workers, svc.Bus, cfg.Jobs)
	handleEmbeddingsMigrate(pool, driver, upgradeMgr, svc.Bus)
	handleReproject(pool, driver, upgradeMgr, svc.Bus)
	handleReprocess(pool, st)

	// 10a. Wire fan-out enrichment (bidirectional edges + audit log).
	if cfg.FanOut.Enabled {
		pool.SetFanOut(func(ctx context.Context, objectID string) error {
			_, err := svc.FanOut(ctx, objectID)
			return err
		})
	}

	router := st.Router
	router.Handle("/ws/bus", hubNet.Handler())

	httpSrv := &gohttp.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 30 * time.Second,
	}
	// Reflection advertises the API surface; keep it for private
	// instances only.
	grpcSrv := grpcserver.New(grpcBind, svc,
		grpcserver.WithAuth(st.RouteAuth),
		grpcserver.WithSecurity(st.Security),
		grpcserver.WithEntitlements(st.Entitlements),
		grpcserver.WithReflection(access == config.AccessPrivate),
	)

	// 10c. Write pidfile so dpkms ps can discover this instance.
	// Deferred removal covers both clean shutdown and error paths.
	runDir, err := config.RunDir()
	if err != nil {
		return fmt.Errorf("run dir: %w", err)
	}
	if err := pidfile.CheckNameConflict(runDir, instanceName); err != nil {
		return err
	}
	configPath := cfgFile
	var browserPort int
	if browserMgr != nil {
		browserPort = browserMgr.Port()
	}
	if err := pidfile.Write(runDir, pidfile.Info{
		PID:              os.Getpid(),
		Name:             instanceName,
		ConfigPath:       configPath,
		Port:             port,
		GRPCPort:         grpcPort,
		CookieBridgePort: cookieBridgePort,
		BrowserPort:      browserPort,
		DBPath:           storagePath,
		StartedAt:        time.Now(),
	}); err != nil {
		// Non-fatal: ps won't show this instance but serve still works.
		fmt.Fprintf(os.Stderr, "warning: could not write pidfile: %v\n", err)
	}
	defer pidfile.Remove(runDir, port)

	// 10d. Crash recovery: reset any jobs left in "running" state from a
	// previous crash back to "pending" so they are picked up immediately.
	if n, err := queue.RecoverStale(context.Background(), 0); err != nil {
		return fmt.Errorf("crash recovery: %w", err)
	} else if n > 0 {
		fmt.Printf("Crash recovery: reset %d stale job(s) to pending\n", n)
	}

	// 10e. ADR-070 reindex_auto: a stale FTS signature schedules the
	// re-projection of stored objects (resumes a pending run instead).
	scheduleReproject(context.Background(), os.Stdout, queue, ftsVerify, svc.Bus)

	// Drain timeout: how long components get to stop after a signal or
	// a component failure before serve exits regardless.
	drainTimeout := cfg.Jobs.DrainTimeout
	if drainTimeout == 0 {
		drainTimeout = 30 * time.Second
	}

	// Reminder checker.
	reminderBackend := &remind.ServiceBackend{
		ListDue: func(ctx context.Context, now time.Time) ([]*remind.DueReminder, error) {
			objs, err := svc.ListDueReminders(ctx, now)
			if err != nil {
				return nil, err
			}
			dues := make([]*remind.DueReminder, 0, len(objs))
			for _, o := range objs {
				var title string
				if len(o.Summaries) > 0 {
					title = o.Summaries[0]
					if len(title) > 60 {
						title = title[:57] + "..."
					}
				} else {
					title = o.ID
				}
				dues = append(dues, &remind.DueReminder{
					ID:       o.ID,
					Title:    title,
					RemindAt: *o.RemindAt,
				})
			}
			return dues, nil
		},
		MarkDone: svc.MarkReminded,
	}
	checker := remind.NewChecker(reminderBackend, reminderInterval)

	// Cookie bridge (for browser extension).
	cookieBridge := wsserver.NewCookieBridgeServer(wsserver.NewCookieCache(), cookieBridgeHosts)

	// 11. Run every component as one unit: the first failure, or a
	// shutdown signal, stops all of them (HTTP included) and serve exits
	// within the drain timeout. A failure exits non-zero so a supervisor
	// (launchd KeepAlive, systemd Restart=always) restarts the daemon.
	fmt.Printf("HTTP server listening on %s\n", addr)
	fmt.Printf("gRPC server listening on %s\n", grpcBind)
	fmt.Printf("Cookie bridge listening on ws://%s\n", cookieBridgeAddr)
	fmt.Printf("Worker pool started (%d workers)\n", workers)
	fmt.Println("Watcher manager started")
	fmt.Printf("Reminder checker started (interval: %s)\n", reminderInterval)
	runner := &componentRunner{
		components: []component{
			httpComponent("http", httpSrv, httpLn, drainTimeout),
			{name: "grpc", run: func(ctx context.Context) error { return grpcSrv.Serve(ctx, grpcLn) }},
			{name: "cookie-bridge", run: func(ctx context.Context) error { return cookieBridge.Serve(ctx, cookieLn) }},
			{name: "workers", run: pool.Start},
			{name: "watcher", run: watchMgr.Start},
			{name: "reminders", run: checker.Run},
			// Federation async workers (US-0319): one goroutine per async
			// target; Stop drains them within its own bound.
			{name: "federation", run: func(ctx context.Context) error {
				fedSet.Start(ctx)
				<-ctx.Done()
				stopCtx, stopCancel := context.WithTimeout(context.Background(), 6*time.Second)
				defer stopCancel()
				if err := fedSet.Stop(stopCtx); err != nil {
					fmt.Fprintf(os.Stderr, "warning: federation workers stop: %v\n", err)
				}
				return nil
			}},
		},
		drain: drainTimeout,
		log:   os.Stderr,
	}

	// The signal channel is read for the runner's whole lifetime and
	// released right after, so a signal is never parked on a channel
	// nobody reads: during cleanup below it takes its default action.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	runner.signals = sigCh

	fmt.Println()
	fmt.Println("dPKMS is ready. Press Ctrl+C to stop.")

	runErr := runner.run(context.Background())
	signal.Stop(sigCh)

	// Cleanup.
	driver.Close(context.Background())
	fmt.Println("Storage closed")

	return runErr
}

// resolveInstanceName returns the instance name for this serve invocation.
// Priority: --name flag > derive from DB basename.
// Validates the name is URI-safe (lowercase alphanumeric + hyphens).
func resolveInstanceName(cmd *cobra.Command) (string, error) {
	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		// Derive from DB path: strip directory and known extensions, then slugify.
		dbPath := cfg.Storage.Path
		if dbPath == "" {
			dbPath = "dpkms.db"
		}
		name = instanceNameFromDBPath(dbPath)
	}
	if !pidfile.ValidateName(name) {
		return "", fmt.Errorf(
			"instance name %q is invalid: use lowercase letters, digits, and hyphens (e.g. 'work', 'my-project')",
			name,
		)
	}
	return name, nil
}

// instanceNameFromDBPath derives a URI-safe instance name from a DB file path.
// e.g. "/data/work.db" → "work", "dpkms.sqlite" → "dpkms", "my_work.db" → "my-work".
func instanceNameFromDBPath(dbPath string) string {
	base := filepath.Base(dbPath)
	// Strip known extensions.
	for _, ext := range []string{".sqlite3", ".sqlite", ".db"} {
		if strings.HasSuffix(base, ext) {
			base = strings.TrimSuffix(base, ext)
			break
		}
	}
	// Slugify: lowercase, replace non-alphanumeric runs with hyphens, trim hyphens.
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(base) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevHyphen = false
		} else if !prevHyphen && b.Len() > 0 {
			b.WriteByte('-')
			prevHyphen = true
		}
	}
	name := strings.TrimRight(b.String(), "-")
	if name == "" {
		return "default"
	}
	return name
}

// daemonize re-executes the current binary in the background without the
// --daemon flag, redirecting stdin/stdout/stderr to /dev/null, then returns
// immediately so the calling process (the user's terminal) exits.
func daemonize(cmd *cobra.Command) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("daemon: resolve executable: %w", err)
	}

	// Build args: all os.Args except the --daemon flag itself.
	args := slices.DeleteFunc(os.Args[1:], func(s string) bool {
		return s == "--daemon" || s == "-daemon"
	})

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return fmt.Errorf("daemon: open /dev/null: %w", err)
	}
	defer devNull.Close()

	// #nosec G204,G702 -- self is os.Executable(): the daemon
	// re-execs this very binary to detach. args are assembled from
	// parsed flags, never from request or captured content.
	c := exec.Command(self, args...)
	c.Stdin = devNull
	c.Stdout = devNull
	c.Stderr = devNull
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := c.Start(); err != nil {
		return fmt.Errorf("daemon: start: %w", err)
	}

	fmt.Printf("dpkms started (pid %d)\n", c.Process.Pid)
	return nil
}

// resolveDefaultProfile ensures cfg.Profile.Default is set before the server
// starts. Priority:
//  1. --profile flag (already bound to viper "profile.default" in init())
//  2. profile.default from config file (via cfg.Profile.Default, set by Load())
//  3. If exactly one profile is defined, use it automatically.
//  4. If multiple profiles and none flagged as default, prompt the user to pick.
//
// Non-interactive contexts (no TTY) fall through without error if no default is
// set — the server starts with no active profile.
func resolveDefaultProfile(cmd *cobra.Command) error {
	// Already set via flag or config?
	if viper.GetString("profile.default") != "" {
		return nil
	}

	profiles := cfg.Profile.Profiles
	if len(profiles) == 0 {
		return nil // no profiles defined; nothing to resolve
	}

	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)

	// Single profile: use it automatically.
	if len(names) == 1 {
		viper.Set("profile.default", names[0])
		return nil
	}

	// Multiple profiles, no default — interactive picker (requires a TTY).
	if !isTTY() {
		return nil // headless: start without a default profile
	}

	// Build huh select options: one per profile + a "(none)" option.
	opts := make([]huh.Option[string], 0, len(names)+1)
	for _, name := range names {
		p := profiles[name]
		label := name
		if p.Description != "" {
			label = name + " — " + p.Description
		}
		opts = append(opts, huh.NewOption(label, name))
	}
	opts = append(opts, huh.NewOption("(none)", ""))

	var selected string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("No default profile set. Choose one:").
				Options(opts...).
				Value(&selected),
		),
	)

	if err := form.Run(); err != nil {
		// User canceled (Ctrl+C / Esc) — proceed without a profile. The
		// prompt is a convenience for picking a default; declining it is a
		// valid choice, not a startup failure.
		return nil //nolint:nilerr // declining the optional profile prompt is not a startup failure
	}

	if selected != "" {
		viper.Set("profile.default", selected)
	}
	return nil
}

// isTTY reports whether stdin is an interactive terminal.
func isTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
