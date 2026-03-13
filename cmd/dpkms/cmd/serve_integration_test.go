package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	gohttp "net/http"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	httpserver "github.com/ideacrafterslabs/ctxt/internal/server/http"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func TestServeStartsAndStops(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	queue := jobs.NewQueue(driver.Jobs())
	pipes := builtins.Registry()
	engine := search.NewEngine(driver)
	svc := service.New(driver, queue, pipes, engine, "", nil)

	router := httpserver.NewRouter(svc, false, nil)

	// Find a free port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	httpSrv := &gohttp.Server{
		Addr:    addr,
		Handler: router,
	}

	pool := jobs.NewWorkerPool(queue, pipes, driver, 1, nil, config.JobsConfig{PollInterval: 50 * time.Millisecond, StaleTimeout: 30 * time.Minute, MaxRetries: 3, MaxHops: 5})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		if err := httpSrv.Serve(ln); err != nil && err != gohttp.ErrServerClosed {
			return err
		}
		return nil
	})

	g.Go(func() error {
		return pool.Start(ctx)
	})

	// Wait for server to be ready.
	var resp *gohttp.Response
	for i := 0; i < 50; i++ {
		resp, err = gohttp.Get(fmt.Sprintf("http://%s/health", addr))
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("server never became ready: %v", err)
	}

	// Verify health.
	resp, err = gohttp.Get(fmt.Sprintf("http://%s/health", addr))
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != gohttp.StatusOK {
		t.Errorf("health status: got %d, want 200", resp.StatusCode)
	}

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("health body: got %v", body)
	}

	// Shutdown.
	cancel()
	httpSrv.Shutdown(context.Background())

	done := make(chan error, 1)
	go func() { done <- g.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("shutdown error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down in time")
	}
}
