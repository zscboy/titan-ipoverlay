package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/zeromicro/go-zero/core/logx"
)

func main() {
	var cfgFile string
	flag.StringVar(&cfgFile, "f", "config.json", "path to config file")
	flag.Parse()

	cfg, err := LoadConfig(cfgFile)
	if err != nil {
		logx.Must(err)
	}

	logx.MustSetup(cfg.Log)
	defer logx.Close()

	logx.Info("transfer starting...")

	router := NewRouter(cfg.Backends, cfg.TimeoutDuration)
	defer router.Close()

	limiter := NewLimiterManager(cfg.ReadLimitBytesPerSec, cfg.WriteLimitBytesPerSec, cfg.BurstBytes)

	var socksServer *Socks5Server
	var httpServer *HTTPServer

	if cfg.Socks5Listen != "" {
		socksServer = NewSocks5Server(cfg.Socks5Listen, router, limiter)
		if err := socksServer.Start(); err != nil {
			logx.Must(err)
		}
	}

	if cfg.HTTPListen != "" {
		httpServer = NewHTTPServer(cfg.HTTPListen, router, limiter)
		if err := httpServer.Start(); err != nil {
			logx.Must(err)
		}
	}

	// Wait for exit signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigChan
	logx.Infof("Received signal %v, shutting down...", sig)

	if socksServer != nil {
		socksServer.Stop()
	}
	if httpServer != nil {
		httpServer.Stop()
	}

	logx.Info("test8 stopped.")
}
