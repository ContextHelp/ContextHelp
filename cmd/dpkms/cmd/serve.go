package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	gohttp "net/http"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"sort"
	"syscall"
	"time"

	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/sync/errgroup"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/browser"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/federation"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/policy"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/remind"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/secrets"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	grpcserver "github.com/ideacrafterslabs/ctxt/internal/server/grpc"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	wsserver "github.com/ideacrafterslabs/ctxt/internal/server/ws"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	"github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
	"github.com/ideacrafterslabs/ctxt/internal/watcher"

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
	access, authProvider, err := resolveInboundAuth(cfg, public)
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

	// 2. Init queue.
	queue := jobs.NewQueue(driver.Jobs())
	fmt.Println("Job queue initialized")

	// 3. Init pipeline registry with configured providers and overrides.
	secretsStore, err := secrets.New(cfg.Secrets)
	if err != nil {
		return fmt.Errorf("init secrets: %w", err)
	}
	factory := providers.NewFactory(cfg.Providers, secretsStore)

	// 3b. Browser automation daemon (optional).
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

	pipes := builtins.ConfiguredRegistryWithPipelineOverrides(
		factory,
		cfg.Providers,
		cfg.Pipelines,
		driver.Blobs(),
		cfg.Storage.Blob.Threshold,
		browserClient,
	)
	fmt.Println("Pipeline runtime initialized (with overrides)")

	// 3b. Wire pipeline preflight validation into the queue.
	queue.SetPipelineValidator(func(name string) error {
		_, err := pipes.Get(name)
		return err
	})

	// 4. Init search engine.
	engine := search.NewEngine(driver)

	// 5. Get steps path.
	stepsPath := viper.GetString("steps.path")
	if stepsPath == "" {
		homeDir, _ := os.UserHomeDir()
		stepsPath = homeDir + "/.config/contexthelp/steps"
	}

	// 6. Init service layer.
	bus := events.NewLocalBus()
	events.SetupSubscriber(bus, cfg, config.GetConfigPath(binName))

	// 6.0 ADR-070 §3: verify the FTS index signature on startup, on either
	// backend. Detection only — the reindex worker acts separately. A
	// mismatch (or first boot) logs a warning and emits a bus event; the
	// daemon proceeds.
	var (
		sigDB      *sql.DB
		sigDialect indexsig.Dialect
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
			if res.FirstBoot {
				fmt.Printf("FTS signature: first-boot stamp %s (inputs: %s)\n",
					res.NewHash[:12], res.InputsSummary)
			} else {
				fmt.Fprintf(os.Stderr,
					"warning: FTS signature mismatch: old=%s new=%s inputs=%s — reindex_auto pending\n",
					res.OldHash[:12], res.NewHash[:12], res.InputsSummary,
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
				_ = bus.Publish(context.Background(), ev)
			}
		}
	}

	// 6a. Cross-process event bus hub. Constructed before service.New
	// so the kit/runtime/policy engine can subscribe and the resulting
	// EventPublisher can be wired into domain.Service[Pipeline]
	// inside service.NewWithOptions. Remote apps (aps, tlc) connect to
	// /ws/bus to share events.
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

	// 6b. Wire kit/runtime/policy on the hub bus. Misconfig (bad YAML,
	// unknown topic, broken CEL) fails loud here so the daemon never
	// serves traffic against an unenforced ruleset.
	// policy.Init already prefixes returned errors with "policy:" so
	// we surface them as-is rather than re-wrap and produce
	// "policy: policy: ...".
	// Non-private instances refuse the env principal fallback: a remote
	// caller with no authenticated principal must resolve as anonymous,
	// never as the daemon operator's $USER/$KIT_POLICY_ROLE.
	var polOpts []policy.Option
	if access != config.AccessPrivate {
		polOpts = append(polOpts, policy.WithoutEnvPrincipal())
	}
	pol, err := policy.Init(hubBus, polOpts...)
	if err != nil {
		return err
	}
	defer pol.Close()

	svc := service.NewWithOptions(driver, queue, pipes, engine, stepsPath, bus,
		[]service.Option{service.WithPolicyPublisher(pol.Publisher())},
		*cfg,
	)

	// 6c. Init watcher manager.
	watchMgr := watcher.NewManager(driver.Watches(), svc)

	// 7. Build HTTP router. Inject /healthz probes so the envelope
	// reports the binary's compiled version + serve start time. gRPC
	// probe is wired below once grpcSrv is constructed.
	devCORS := viper.GetBool("server.dev")

	// Upgrade-state manager (ADR-070 §5, T-0580). The shadow file lives
	// next to pidfiles so CLI-side banner injection finds it without an
	// HTTP roundtrip. RunDir errors are non-fatal: a missing run dir
	// just disables the shadow (the in-memory state still feeds /healthz).
	var upgradeMgr *upgrade.Manager
	if runDir, runDirErr := config.RunDir(); runDirErr == nil {
		upgradeMgr = upgrade.NewManager(filepath.Join(runDir, "upgrade-state.json"))
	} else {
		upgradeMgr = upgrade.NewManager("")
	}

	healthProbes := httpserver.HealthzProbes{
		Version: version,
		Started: time.Now(),
		Upgrade: func(_ context.Context) *httpserver.UpgradeSnapshot {
			snap := upgradeMgr.Snapshot()
			if snap.State == upgrade.StateIdle {
				return nil
			}
			out := &httpserver.UpgradeSnapshot{
				State:      string(snap.State),
				Bucket:     string(snap.Bucket),
				Progress:   snap.Progress,
				Done:       snap.Done,
				Total:      snap.Total,
				EtaSeconds: snap.EtaSeconds,
				LastError:  snap.LastError,
			}
			if !snap.StartedAt.IsZero() {
				out.StartedAt = snap.StartedAt.UTC().Format(time.RFC3339)
			}
			return out
		},
	}
	// Non-private instances authenticate the whole /api/v1 route table
	// (MCP mount included) through the provider-agnostic middleware.
	var routeAuth authn.Provider
	if access != config.AccessPrivate {
		routeAuth = authProvider
	}
	// Security event emitter: auth failures and ACL denials feed the
	// audit log (event_class=security) plus optional webhook/SMTP
	// alerting from config. Wired on every instance — policy denials
	// matter on private ones too.
	secEmitter := security.New(security.ConfigFromAlerts(cfg.Security.Alerts), driver.AuditLog())

	// Inbound entitlement/metering gate for the entity-serving surface.
	// Only authenticated (non-private) instances construct one: grants
	// are entitlement rows keyed by principal ID, quotas come from
	// server.quotas config. nil on private instances = no gating.
	var inboundGate *registry.InboundGate
	if routeAuth != nil {
		inboundGate = registry.NewInboundGate(driver.Entitlements(), driver.Metering())
		for _, q := range cfg.Server.Quotas {
			inboundGate.SetQuota(q.Principal, storage.MeteringEventType(q.Event), storage.QuotaConfig{
				Limit:  q.Limit,
				WarnAt: q.WarnAt,
			})
		}
	}

	// Serve-time federation-credential validation: on non-private
	// instances the push route is gated per federation.token, never by
	// a principal token alone; without one the route refuses requests.
	requireFedCred := access != config.AccessPrivate
	if requireFedCred && cfg.Federation.Token == "" {
		fmt.Printf("Federation push: refused — no federation.token configured (mandatory on %s instances)\n", access)
	}

	router := httpserver.NewRouterWithConfig(svc, httpserver.RouterConfig{
		DevCORS:                     devCORS,
		Watcher:                     watchMgr,
		Probes:                      healthProbes,
		Auth:                        routeAuth,
		Security:                    secEmitter,
		Entitlements:                inboundGate,
		RequireFederationCredential: requireFedCred,
		RedactHealthz:               access == config.AccessPublic,
	})
	router.Handle("/ws/bus", hubNet.Handler())

	// 8. Determine bind address. Only private instances stay on
	// loopback; protected/public bind all interfaces (and are already
	// guaranteed to have inbound auth configured above).
	bind := "127.0.0.1"
	if access != config.AccessPrivate {
		bind = "0.0.0.0"
	}

	// 9. Auto-assign HTTP port if preferred is busy.
	port, err = findFreePort(bind, port)
	if err != nil {
		return fmt.Errorf("http port: %w", err)
	}
	addr := fmt.Sprintf("%s:%d", bind, port)

	// 9a. Create HTTP server.
	httpSrv := &gohttp.Server{
		Addr:    addr,
		Handler: router,
	}

	// 9b. Auto-assign gRPC port if preferred is busy.
	grpcPort, err = findFreePort(bind, grpcPort)
	if err != nil {
		return fmt.Errorf("grpc port: %w", err)
	}
	grpcBind := fmt.Sprintf("%s:%d", bind, grpcPort)
	// Reflection advertises the API surface; keep it for private
	// instances only.
	grpcSrv := grpcserver.New(grpcBind, svc,
		grpcserver.WithAuth(routeAuth),
		grpcserver.WithSecurity(secEmitter),
		grpcserver.WithEntitlements(inboundGate),
		grpcserver.WithReflection(access == config.AccessPrivate),
	)

	// 9c. Auto-assign cookie-bridge port if preferred is busy.
	cookieBridgePort, err := findFreePort("127.0.0.1", wsserver.DefaultCookieBridgePort)
	if err != nil {
		return fmt.Errorf("cookie bridge port: %w", err)
	}
	cookieBridgeAddr := fmt.Sprintf("127.0.0.1:%d", cookieBridgePort)

	// 10. Init worker pool.
	pool := jobs.NewWorkerPool(queue, pipes, driver, workers, svc.Bus, cfg.Jobs)

	// 10pre. Init federation worker set. Cycle detection runs here; an
	// invalid topology fails serve before any port is bound (US-0318 AC).
	fedSet, err := federation.New(*cfg, driver)
	if err != nil {
		return fmt.Errorf("federation: %w", err)
	}
	if n := fedSet.Len(); n > 0 {
		fmt.Printf("Federation: %d async target(s) configured\n", n)
	}

	// 10a. Wire fan-out enrichment (bidirectional edges + audit log).
	if cfg.FanOut.Enabled {
		pool.SetFanOut(func(ctx context.Context, objectID string) error {
			_, err := svc.FanOut(ctx, objectID)
			return err
		})
	}

	// 10b. Bind HTTP port early so port conflicts fail before we write the
	// pidfile or start any background goroutines.
	httpLn, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	// 10c. Write pidfile so dpkms ps can discover this instance.
	// Deferred removal covers both clean shutdown and error paths.
	runDir, err := config.RunDir()
	if err != nil {
		httpLn.Close()
		return fmt.Errorf("run dir: %w", err)
	}
	if err := pidfile.CheckNameConflict(runDir, instanceName); err != nil {
		httpLn.Close()
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

	// 11. Start everything via errgroup.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)

	// HTTP server — listener already bound above.
	g.Go(func() error {
		fmt.Printf("HTTP server listening on %s\n", addr)
		if err := httpSrv.Serve(httpLn); err != nil && err != gohttp.ErrServerClosed {
			return err
		}
		return nil
	})

	// gRPC server.
	g.Go(func() error {
		fmt.Printf("gRPC server listening on %s\n", grpcBind)
		return grpcSrv.Start(ctx)
	})

	// Worker pool.
	g.Go(func() error {
		fmt.Printf("Worker pool started (%d workers)\n", workers)
		return pool.Start(ctx)
	})

	// Watcher manager.
	g.Go(func() error {
		fmt.Println("Watcher manager started")
		return watchMgr.Start(ctx)
	})

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
	g.Go(func() error {
		fmt.Printf("Reminder checker started (interval: %s)\n", reminderInterval)
		return checker.Run(ctx)
	})

	// Cookie bridge (for browser extension).
	cookieCache := wsserver.NewCookieCache()
	cookieBridge := wsserver.NewCookieBridgeServer(cookieCache, cookieBridgeAddr)
	g.Go(func() error {
		fmt.Printf("Cookie bridge listening on ws://%s\n", cookieBridgeAddr)
		return cookieBridge.Start(ctx)
	})

	// Federation async workers (US-0319). Spawns one goroutine per async
	// target; honours ctx for graceful shutdown (Stop drains within 5s).
	fedSet.Start(ctx)
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer stopCancel()
		if err := fedSet.Stop(stopCtx); err != nil {
			fmt.Fprintf(os.Stderr, "warning: federation workers stop: %v\n", err)
		}
	}()

	// Drain timeout: how long in-flight jobs get to finish after signal.
	drainTimeout := cfg.Jobs.DrainTimeout
	if drainTimeout == 0 {
		drainTimeout = 30 * time.Second
	}

	// Wait for shutdown signal.
	g.Go(func() error {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
		select {
		case sig := <-sigChan:
			fmt.Println()
			if sig == syscall.SIGHUP {
				fmt.Println("Received SIGHUP — restarting...")
			} else {
				fmt.Println("Shutting down gracefully...")
			}
		case <-ctx.Done():
			return nil
		}

		// Stop accepting new jobs and requests.
		cancel()

		// Give in-flight jobs time to finish, then shut down HTTP/gRPC.
		shutCtx, shutCancel := context.WithTimeout(context.Background(), drainTimeout)
		defer shutCancel()
		fmt.Printf("Draining workers (up to %s)...\n", drainTimeout)
		httpSrv.Shutdown(shutCtx)
		// gRPC server stops via ctx cancellation in grpcSrv.Start.
		return nil
	})

	fmt.Println()
	fmt.Println("dPKMS is ready. Press Ctrl+C to stop.")

	if err := g.Wait(); err != nil {
		return err
	}

	// Cleanup.
	driver.Close(context.Background())
	fmt.Println("Storage closed")

	return nil
}

// resolveInboundAuth folds the --public flag into the loaded config,
// enforces the access-class rules, and constructs the configured
// authentication provider. Returns the effective access class and the
// provider (nil when no auth is configured, which is only legal for
// private instances). Any misconfiguration — unknown class, explicit
// private + --public, non-private without credentials, broken provider
// config — refuses serve before a single port is bound.
func resolveInboundAuth(c *config.Config, publicFlag bool) (string, authn.Provider, error) {
	folded := *c
	folded.Server.Public = folded.Server.Public || publicFlag
	if err := folded.ValidateAccess(); err != nil {
		return "", nil, err
	}
	access := folded.Server.EffectiveAccess()

	provider, err := authn.FromConfig(c.Server.Auth)
	if err != nil {
		return "", nil, err
	}
	if access != config.AccessPrivate && provider == nil {
		// Unreachable while ValidateAccess covers credential presence;
		// kept as a hard stop so a validation regression can never
		// expose an unauthenticated non-private listener.
		return "", nil, fmt.Errorf("config: %s instance requires inbound authentication (server.auth)", access)
	}
	return access, provider, nil
}

// findFreePort tries to bind preferred on the given bind address.
// If preferred is busy, it asks the OS for any free port.
// The listener is closed immediately; the caller owns the port convention.
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

func findFreePort(bind string, preferred int) (int, error) {
	addr := fmt.Sprintf("%s:%d", bind, preferred)
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		ln.Close()
		return preferred, nil
	}
	// Preferred port is busy — let the OS pick one on the same bind
	// address, so the returned port is actually bindable there.
	ln, err = net.Listen("tcp", fmt.Sprintf("%s:0", bind))
	if err != nil {
		return 0, fmt.Errorf("no free port available: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port, nil
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
		// User cancelled (Ctrl+C / Esc) — proceed without a profile.
		return nil
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
