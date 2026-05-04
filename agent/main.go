package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/technonext/chowkidar/agent/collect"
	"github.com/technonext/chowkidar/agent/config"
	"github.com/technonext/chowkidar/agent/logs"
	"github.com/technonext/chowkidar/agent/report"
	"github.com/technonext/chowkidar/agent/types"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if err := godotenv.Load(); err != nil {
		slog.Debug("no .env loaded, relying on process env", "err", err)
	} else {
		slog.Info("loaded .env from cwd")
	}

	cfg := config.Load()

	if cfg.Identity == "" {
		slog.Error("AGENT_IDENTITY is required (set in env or .env)")
		os.Exit(1)
	}
	if cfg.Token == "" {
		slog.Error("AGENT_TOKEN is required (set in env or .env)")
		os.Exit(1)
	}
	if cfg.ServerURL == "" {
		slog.Error("SERVER_URL is required (set in env or .env)")
		os.Exit(1)
	}

	systemCollector := collect.NewSystemCollector()

	dockerCollector, err := collect.NewDockerCollector()
	if err != nil {
		slog.Error("docker collector init failed", "err", err)
		os.Exit(1)
	}

	reporter := report.NewReporter(cfg.ServerURL, cfg.Token)
	defer reporter.Close()

	slog.Info("agent started",
		"identity", cfg.Identity,
		"server", cfg.ServerURL,
		"interval", cfg.CollectInterval,
		"batch_ms", cfg.LogBatchMS,
		"batch_bytes", cfg.LogBatchBytes,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logCollector := logs.New(dockerCollector.Client())
	logShipper := logs.NewShipper(cfg.ServerURL, cfg.Token, cfg.LogBatchMS, cfg.LogBatchBytes)
	defer logShipper.Close()

	go logCollector.Run(ctx)
	go logShipper.Run(ctx, logCollector.Out())

	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		var last uint64
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				cur := logCollector.Dropped()
				if delta := cur - last; delta > 0 {
					slog.Warn("log lines dropped", "delta", delta, "total", cur)
				}
				last = cur
			}
		}
	}()

	ticker := time.NewTicker(cfg.CollectInterval)
	defer ticker.Stop()

	inFlight := make(chan struct{}, 1)
	inFlight <- struct{}{}

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down")
			return

		case <-ticker.C:
			select {
			case token := <-inFlight:
				go func(tok struct{}) {
					defer func() { inFlight <- tok }()
					metrics := collectAll(systemCollector, dockerCollector)
					metrics.Timestamp = time.Now()
					metrics.Identity = cfg.Identity
					metrics.ServerName = cfg.Identity

					if err := reporter.Send(ctx, metrics); err != nil {
						slog.Error("report send failed", "err", err)
					} else {
						slog.Info("report sent",
							"cpu_pct", metrics.System.CPUPercent,
							"mem_used_gb", metrics.System.MemUsedGB,
							"mem_total_gb", metrics.System.MemTotalGB,
							"containers", len(metrics.Containers),
						)
					}
				}(token)
			default:
				slog.Warn("skipping tick, previous collection in flight")
			}
		}
	}
}

func collectAll(sys *collect.SystemCollector, docker *collect.DockerCollector) *types.Report {
	systemMetrics, err := sys.Collect()
	if err != nil {
		slog.Warn("system collect failed, sending zero values", "err", err)
	}

	containers, err := docker.Collect()
	if err != nil {
		slog.Warn("docker collect failed, sending empty list", "err", err)
		containers = nil
	}

	return &types.Report{
		System:     systemMetrics,
		Containers: containers,
	}
}
