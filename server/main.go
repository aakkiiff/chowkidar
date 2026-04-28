package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/technonext/chowkidar/server/alert"
	"github.com/technonext/chowkidar/server/api"
	"github.com/technonext/chowkidar/server/config"
	"github.com/technonext/chowkidar/server/logbroker"
	"github.com/technonext/chowkidar/server/logstore"
	"github.com/technonext/chowkidar/server/probe"
	"github.com/technonext/chowkidar/server/store"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	loadDotenv()

	cfg := config.Load()

	db, err := store.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()

	logs, err := logstore.New(logstore.Config{
		Dir:           cfg.LogDir,
		MaxFileBytes:  int64(cfg.LogMaxFileMB) * 1024 * 1024,
		MaxRotations:  cfg.LogMaxRotations,
		RetentionDays: cfg.LogRetentionDays,
	})
	if err != nil {
		log.Fatalf("log store: %v", err)
	}
	defer logs.Close()

	broker := logbroker.New()

	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.AdminPass), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("hash admin password: %v", err)
	}
	if err := db.CreateUser(cfg.AdminUser, string(hash)); err != nil {
		log.Fatalf("create admin user: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Alert evaluator: every 15s reads latest system metrics + rules and
	// fires webhooks for breaches sustained past the configured window.
	// The broker fans live events out to SSE subscribers (frontend toasts).
	alertBroker := alert.NewBroker()
	evaluator := alert.NewEvaluator(db, db, alert.NewPoster(), alertBroker, 15*time.Second)
	go evaluator.Run(ctx)

	// Endpoint prober: hourly prune + interval-driven HTTP probes.
	prober := probe.New(db, alertBroker, alert.NewPoster())
	go prober.Run(ctx)
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := db.PruneEndpointProbes(); err != nil {
					log.Printf("probe prune: %v", err)
				}
				if err := db.PruneEndpointIncidents(); err != nil {
					log.Printf("incident prune: %v", err)
				}
			}
		}
	}()

	// Background goroutine: rolls up raw metrics to 1-minute averages and
	// prunes old data once per minute.
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := db.RollupAndPrune(cfg.RetentionDaysContainers); err != nil {
					log.Printf("rollup: %v", err)
				}
			}
		}
	}()

	// One-shot orphan log prune on startup. Catches dirs whose owning agent
	// was deleted while the server was down — the hourly ticker would leave
	// them around for up to an hour otherwise.
	if ids, err := db.AgentIDs(); err == nil {
		logs.PruneOrphans(ids)
	}
	logs.PruneOld()

	// Periodic flush + retention for log files.
	go func() {
		flush := time.NewTicker(5 * time.Second)
		prune := time.NewTicker(time.Hour)
		defer flush.Stop()
		defer prune.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-flush.C:
				logs.Flush()
			case <-prune.C:
				logs.PruneOld()
				if ids, err := db.AgentIDs(); err == nil {
					logs.PruneOrphans(ids)
				}
			}
		}
	}()

	handler := api.NewHandler(db, logs, broker, alertBroker, cfg.JWTSecret)
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      handler.Routes(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // SSE handlers stream indefinitely; per-handler timeouts guard IO
		IdleTimeout:  120 * time.Second,
		// BaseContext wires every request's Context to our shutdown signal.
		// When `ctx` is cancelled, in-flight SSE loops and the ingest scanner
		// observe r.Context().Done() and return promptly, letting Shutdown
		// finish well inside the drain deadline.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	go func() {
		<-ctx.Done()
		log.Println("shutting down — draining active streams...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
		// Flush buffered log writes before the process exits so the last
		// batch from in-flight POSTs lands on disk.
		logs.Flush()
	}()

	log.Printf("chowkidar server listening on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}

// loadDotenv tries several predictable locations so the server reads its
// config regardless of the caller's cwd:
//   1. $CHOWKIDAR_ENV  (explicit override)
//   2. ./.env          (cwd — docker compose with workdir)
//   3. ./server/.env   (running `go run ./server` from repo root)
//   4. <binary dir>/.env  (installed binary alongside its config)
// First hit wins. Silent on miss — env vars from the process environment
// still take precedence and the app has sensible defaults either way.
func loadDotenv() {
	candidates := []string{}
	if v := os.Getenv("CHOWKIDAR_ENV"); v != "" {
		candidates = append(candidates, v)
	}
	candidates = append(candidates, ".env", filepath.Join("server", ".env"))
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), ".env"))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		if err := godotenv.Load(p); err == nil {
			log.Printf("loaded env from %s", p)
			return
		}
	}
}
