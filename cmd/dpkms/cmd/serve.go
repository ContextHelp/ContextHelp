package cmd

import (
	"context"
	"fmt"
	"net"
	gohttp "net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/sync/errgroup"

	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	wsserver "github.com/ideacrafterslabs/ctxt/internal/server/ws"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/watcher"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start background worker and API server",
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

	// Bind flags to viper
	viper.BindPFlag("server.port", serveCmd.Flags().Lookup("port"))
	viper.BindPFlag("server.grpc_port", serveCmd.Flags().Lookup("grpc-port"))
	viper.BindPFlag("server.workers", serveCmd.Flags().Lookup("workers"))
	viper.BindPFlag("server.public", serveCmd.Flags().Lookup("public"))
	viper.BindPFlag("profile.default", serveCmd.Flags().Lookup("profile"))
	viper.BindPFlag("steps.path", serveCmd.Flags().Lookup("steps-path"))
	viper.BindPFlag("server.dev", serveCmd.Flags().Lookup("dev"))
}

func runServe(cmd *cobra.Command, args []string) error {
	port := viper.GetInt("server.port")
	workers := viper.GetInt("server.workers")
	public := viper.GetBool("server.public")

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

	// 3. Init pipeline registry with configured providers.
	factory := providers.NewFactory(cfg.Providers)
	pipes := builtins.ConfiguredRegistryWithOpts(builtins.BuildOpts{
		Factory:       factory,
		BlobThreshold: cfg.Storage.Blob.Threshold,
	})
	fmt.Println("Pipeline runtime initialized")

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

	// 10. Init worker pool.
	pool := jobs.NewWorkerPool(queue, pipes, driver, workers, svc.Bus, cfg.Jobs)

	// 11. Start everything via errgroup.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)

	// HTTP server.
	g.Go(func() error {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("listen: %w", err)
		}
		fmt.Printf("HTTP server listening on %s\n", addr)
		if err := httpSrv.Serve(ln); err != nil && err != gohttp.ErrServerClosed {
			return err
		}
		return nil
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

	// Cookie bridge (for browser extension).
	cookieCache := wsserver.NewCookieCache()
	cookieBridge := wsserver.NewCookieBridgeServer(cookieCache)
	g.Go(func() error {
		fmt.Println("Cookie bridge listening on ws://127.0.0.1:9377")
		return cookieBridge.Start(ctx)
	})

	// Wait for shutdown signal.
	g.Go(func() error {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		select {
		case <-sigChan:
			fmt.Println()
			fmt.Println("Shutting down gracefully...")
		case <-ctx.Done():
		}
		cancel()
		httpSrv.Shutdown(context.Background())
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
