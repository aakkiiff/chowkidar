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
	// Load .env if present. Missing file is fine — vars may come from
	// docker-compose env_file injection or the host environment.
	if err := godotenv.Load(); err != nil {
		log.Printf("[boot] no .env loaded (%v) — relying on process env", err)
	} else {
		log.Printf("[boot] loaded .env from cwd")
	}

	cfg := config.Load()

	if cfg.Identity == "" {
		log.Fatal("[boot] AGENT_IDENTITY is required (set in env or .env)")
	}
	if cfg.Token == "" {
		log.Fatal("[boot] AGENT_TOKEN is required (set in env or .env)")
	}
	if cfg.ServerURL == "" {
		log.Fatal("[boot] SERVER_URL is required (set in env or .env)")
	}

	systemCollector := collect.NewSystemCollector()

	dockerCollector, err := collect.NewDockerCollector()
	if err != nil {
		log.Fatalf("docker collector: %v", err)
	}

	reporter := report.NewReporter(cfg.ServerURL, cfg.Token)
	defer reporter.Close()

	log.Printf("[boot] agent started: identity=%s server=%s interval=%v batch=%v/%dB",
		cfg.Identity, cfg.ServerURL, cfg.CollectInterval, cfg.LogBatchMS, cfg.LogBatchBytes)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logCollector := logs.New(dockerCollector.Client())

	logShipper := logs.NewShipper(cfg.ServerURL, cfg.Token, cfg.LogBatchMS, cfg.LogBatchBytes)
	defer logShipper.Close()

	go logCollector.Run(ctx)
	go logShipper.Run(ctx, logCollector.Out())

	// Surface log drops every 30s. Without this the agent would lose lines
	// silently under back-pressure (server slow / down + drop-oldest).
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
					log.Printf("[logs] dropped %d lines in last 30s (total=%d) — server slow or buffer full",
						delta, cur)
				}
				last = cur
			}
		}
	}()

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

			if err := reporter.Send(ctx, metrics); err != nil {
				log.Printf("[report] send failed: %v", err)
			} else {
				log.Printf("[report] ok: cpu=%.1f%% mem=%.1f/%.1fGB containers=%d",
					metrics.System.CPUPercent,
					metrics.System.MemUsedGB, metrics.System.MemTotalGB,
					len(metrics.Containers))
			}
		}
	}
}

// collectAll gathers system + docker metrics. Each collector's failure is
// logged and the report goes ahead with whatever succeeded — losing one
// signal shouldn't block the other from reaching the server.
func collectAll(sys *collect.SystemCollector, docker *collect.DockerCollector) *types.Report {
	systemMetrics, err := sys.Collect()
	if err != nil {
		log.Printf("[collect] system: %v (sending zero values)", err)
	}

	containers, err := docker.Collect()
	if err != nil {
		log.Printf("[collect] docker: %v (sending empty container list)", err)
		containers = nil
	}

	return &types.Report{
		System:     systemMetrics,
		Containers: containers,
	}
}
