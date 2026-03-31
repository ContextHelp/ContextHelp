package cmd

import (
	"context"
	"fmt"
	"net"
	gohttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/sync/errgroup"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/remind"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/secrets"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	grpcserver "github.com/ideacrafterslabs/ctxt/internal/server/grpc"
	wsserver "github.com/ideacrafterslabs/ctxt/internal/server/ws"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/watcher"
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

  # Start with custom ports
  dpkms serve --port 8080 --grpc-port 9090

  # Start with more workers
  dpkms serve --workers 8

  # Allow remote connections
  dpkms serve --public

  # Use specific profile
  dpkms serve --profile founder`,
	RunE: runServe,
}

func init() {
	rootCmd.AddCommand(serveCmd)

	// Server flags
	serveCmd.Flags().Int("port", 8080, "HTTP port")
	serveCmd.Flags().Int("grpc-port", 9090, "gRPC port")
	serveCmd.Flags().Int("workers", 4, "number of worker threads")
	serveCmd.Flags().Bool("public", false, "allow remote connections")
	serveCmd.Flags().String("profile", "", "default focus profile")
	serveCmd.Flags().String("steps-path", "", "path to external steps directory")
	serveCmd.Flags().Bool("dev", false, "enable CORS for Vite dev server (http://localhost:5173)")
	serveCmd.Flags().Duration("reminder-interval", time.Minute, "how often to check for due reminders")

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
	port := viper.GetInt("server.port")
	grpcPort := viper.GetInt("server.grpc_port")
	workers := viper.GetInt("server.workers")
	public := viper.GetBool("server.public")
	reminderInterval := viper.GetDuration("server.reminder_interval")

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
	secretsResolver, err := secrets.NewResolver(cfg.Secrets)
	if err != nil {
		return fmt.Errorf("init secrets: %w", err)
	}
	factory := providers.NewFactory(cfg.Providers, secretsResolver)
	pipes := builtins.ConfiguredRegistryWithPipelineOverrides(
		factory,
		cfg.Providers,
		cfg.Pipelines,
		driver.Blobs(),
		cfg.Storage.Blob.Threshold,
	)
	fmt.Println("Pipeline runtime initialized (with overrides)")

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
	svc := service.New(driver, queue, pipes, engine, stepsPath, bus, *cfg)

	// 6b. Init watcher manager.
	watchMgr := watcher.NewManager(driver.Watches(), svc)

	// 7. Build HTTP router.
	devCORS := viper.GetBool("server.dev")
	router := httpserver.NewRouter(svc, devCORS, watchMgr)

	// 8. Determine bind address.
	bind := "127.0.0.1"
	if public {
		bind = "0.0.0.0"
	}
	addr := fmt.Sprintf("%s:%d", bind, port)

	// 9. Create HTTP server.
	httpSrv := &gohttp.Server{
		Addr:    addr,
		Handler: router,
	}

	// 9b. Create gRPC server.
	grpcBind := fmt.Sprintf("%s:%d", bind, grpcPort)
	grpcSrv := grpcserver.New(grpcBind, svc)

	// 10. Init worker pool.
	pool := jobs.NewWorkerPool(queue, pipes, driver, workers, svc.Bus, cfg.Jobs)

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
	if err := pidfile.Write(runDir, pidfile.Info{
		PID:       os.Getpid(),
		Port:      port,
		GRPCPort:  grpcPort,
		DBPath:    storagePath,
		StartedAt: time.Now(),
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
	cookieBridge := wsserver.NewCookieBridgeServer(cookieCache)
	g.Go(func() error {
		fmt.Println("Cookie bridge listening on ws://127.0.0.1:9377")
		return cookieBridge.Start(ctx)
	})

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
