package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yuuki/slurm-gpu-node-guard/internal/config"
	"github.com/yuuki/slurm-gpu-node-guard/internal/daemon"
	"github.com/yuuki/slurm-gpu-node-guard/internal/engine"
	"github.com/yuuki/slurm-gpu-node-guard/internal/telemetry"
)

func main() {
	fs := flag.NewFlagSet("guardd", flag.ExitOnError)
	configPath := fs.String("config", "configs/policy.example.yaml", "path to config YAML")
	socketPath := fs.String("socket", "", "override unix socket path")
	fs.Parse(os.Args[1:])

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{}))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdown, err := telemetry.Setup(ctx, "slurm-gpu-node-guard/guardd")
	if err != nil {
		logger.Error("telemetry setup failed", "error", err)
	}
	defer func() {
		timeoutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if shutdown != nil {
			_ = shutdown(timeoutCtx)
		}
	}()

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("load config failed", "error", err)
		os.Exit(1)
	}
	if *socketPath != "" {
		cfg.SocketPath = *socketPath
	}
	eng, err := engine.New(&cfg.Policy, cfg.Plugins)
	if err != nil {
		logger.Error("create engine failed", "error", err)
		os.Exit(1)
	}
	logger.Info("starting guard daemon", "socket", cfg.SocketPath)
	if err := daemon.NewServer(cfg.SocketPath, eng).Run(ctx); err != nil && err != context.Canceled {
		logger.Error("daemon exited with error", "error", err)
		os.Exit(1)
	}
}
