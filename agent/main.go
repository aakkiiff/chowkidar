package main

import (
	"context"
	"log"
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
	// Load .env if present (ignored when env vars are injected by docker-compose)
	godotenv.Load()

	cfg := config.Load()

	if cfg.Identity == "" {
		log.Fatal("AGENT_IDENTITY is required")
	}
	if cfg.Token == "" {
		log.Fatal("AGENT_TOKEN is required")
	}

	systemCollector := collect.NewSystemCollector()

	dockerCollector, err := collect.NewDockerCollector()
	if err != nil {
		log.Fatalf("docker collector: %v", err)
	}

	reporter := report.NewReporter(cfg.ServerURL, cfg.Token)
	defer reporter.Close()

	log.Printf("agent started: identity=%s server=%s interval=%v",
		cfg.Identity, cfg.ServerURL, cfg.CollectInterval)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logCollector := logs.New(dockerCollector.Client())

	logShipper := logs.NewShipper(cfg.ServerURL, cfg.Token, cfg.LogBatchMS, cfg.LogBatchBytes)
	defer logShipper.Close()

	go logCollector.Run(ctx)
	go logShipper.Run(ctx, logCollector.Out())

	ticker := time.NewTicker(cfg.CollectInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("shutting down")
			return

		case <-ticker.C:
			metrics := collectAll(systemCollector, dockerCollector)
			metrics.Timestamp = time.Now()
			metrics.Identity = cfg.Identity
			// ServerName is the human-readable identity; server may use it as display name
			metrics.ServerName = cfg.Identity

			if err := reporter.Send(metrics); err != nil {
				log.Printf("report failed: %v", err)
			} else {
				log.Printf("reported: cpu=%.1f%% mem=%.1f/%.1fGB containers=%d",
					metrics.System.CPUPercent,
					metrics.System.MemUsedGB, metrics.System.MemTotalGB,
					len(metrics.Containers))
			}
		}
	}
}

func collectAll(sys *collect.SystemCollector, docker *collect.DockerCollector) *types.Report {
	systemMetrics, err := sys.Collect()
	if err != nil {
		log.Printf("system collect: %v", err)
	}

	containers, err := docker.Collect()
	if err != nil {
		log.Printf("docker collect: %v", err)
	}

	return &types.Report{
		System:     systemMetrics,
		Containers: containers,
	}
}
