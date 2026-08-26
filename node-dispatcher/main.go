package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

func main() {
	configFile := flag.String("f", "config.yaml", "path to config file")
	flag.Parse()

	cfg, err := LoadConfig(*configFile)
	if err != nil {
		logx.Must(fmt.Errorf("failed to load configuration from '%s': %w", *configFile, err))
	}

	logx.MustSetup(cfg.Log)
	defer logx.Close()

	twoLevelCache := NewTwoLevelCache(cfg.Redis, cfg.L1Cache)
	dispatcher := NewDispatcherServer(*configFile, cfg, twoLevelCache)

	server := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: dispatcher,
	}

	logx.Infof("Node Dispatcher starting on %s (Cells: %d, L1 Cache TTL: %ds, Capacity: %d)",
		cfg.ListenAddr, len(cfg.Cells), cfg.L1Cache.TTLSeconds, cfg.L1Cache.Capacity)

	// Graceful shutdown handling
	serverErrChan := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrChan <- err
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrChan:
		logx.Must(fmt.Errorf("HTTP server error: %w", err))
	case sig := <-sigChan:
		logx.Infof("Received signal %v, initiating graceful shutdown...", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			logx.Errorf("HTTP server graceful shutdown error: %v", err)
		} else {
			logx.Info("HTTP server stopped gracefully.")
		}
	}
}
