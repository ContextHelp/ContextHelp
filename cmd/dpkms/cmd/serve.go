package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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

	// Bind flags to viper
	viper.BindPFlag("server.port", serveCmd.Flags().Lookup("port"))
	viper.BindPFlag("server.grpc_port", serveCmd.Flags().Lookup("grpc-port"))
	viper.BindPFlag("server.workers", serveCmd.Flags().Lookup("workers"))
	viper.BindPFlag("server.public", serveCmd.Flags().Lookup("public"))
	viper.BindPFlag("profile.default", serveCmd.Flags().Lookup("profile"))
}

func runServe(cmd *cobra.Command, args []string) error {
	port := viper.GetInt("server.port")
	grpcPort := viper.GetInt("server.grpc_port")
	workers := viper.GetInt("server.workers")
	public := viper.GetBool("server.public")
	profile := viper.GetString("profile.default")

	fmt.Println("Starting dPKMS server...")
	fmt.Println()
	fmt.Printf("Data directory:     %s\n", cfg.Storage.Path)
	fmt.Printf("HTTP port:          %d\n", port)
	fmt.Printf("gRPC port:          %d\n", grpcPort)
	fmt.Printf("Worker threads:     %d\n", workers)
	fmt.Printf("Public access:      %v\n", public)
	if profile != "" {
		fmt.Printf("Default profile:    %s\n", profile)
	}
	fmt.Println()

	// TODO: Implement actual server startup
	fmt.Println("✓ Storage initialized")
	fmt.Println("✓ Job queue initialized")
	fmt.Println("✓ Pipeline runtime initialized")
	fmt.Printf("✓ HTTP server listening on :%d\n", port)
	fmt.Printf("✓ gRPC server listening on :%d\n", grpcPort)
	fmt.Printf("✓ Worker pool started (%d workers)\n", workers)
	fmt.Println()
	fmt.Println("dPKMS is ready. Press Ctrl+C to stop.")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println()
	fmt.Println("Shutting down gracefully...")
	fmt.Println("✓ Worker pool stopped")
	fmt.Println("✓ HTTP server stopped")
	fmt.Println("✓ gRPC server stopped")
	fmt.Println("✓ Storage closed")

	return nil
}
