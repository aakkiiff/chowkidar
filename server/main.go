package main

import (
	"context"
	"fmt"
	"log/slog"
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
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	loadDotenv()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}

	db, err := store.New(cfg.DBPath, time.Duration(cfg.RawRetentionMinutes)*time.Minute)
	if err != nil {
		slog.Error("database init failed", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	logStore, err := logstore.New(logstore.Config{
		Dir:           cfg.LogDir,
		MaxFileBytes:  int64(cfg.LogMaxFileMB) * 1024 * 1024,
		MaxRotations:  cfg.LogMaxRotations,
		RetentionDays: cfg.LogRetentionDays,
	})
	if err != nil {
		slog.Error("log store init failed", "err", err)
		os.Exit(1)
	}
	defer logStore.Close()

	broker := logbroker.New()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	alertBroker := alert.NewBroker()
	evaluator := alert.NewEvaluator(db, db, alert.NewPoster(), alertBroker, 15*time.Second)
	go evaluator.Run(ctx)

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
					slog.Warn("probe prune failed", "err", err)
				}
				if err := db.PruneEndpointIncidents(); err != nil {
					slog.Warn("incident prune failed", "err", err)
				}
			}
		}
	}()

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := db.RollupAndPrune(cfg.RetentionDaysContainers); err != nil {
					slog.Warn("rollup failed", "err", err)
				}
			}
		}
	}()

	if ids, err := db.AgentIDs(); err == nil {
		logStore.PruneOrphans(ids)
	}
	logStore.PruneOld()

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
				logStore.Flush()
			case <-prune.C:
				logStore.PruneOld()
				if ids, err := db.AgentIDs(); err == nil {
					logStore.PruneOrphans(ids)
				}
			}
		}
	}()

	handler := api.NewHandler(db, logStore, broker, alertBroker, cfg.JWTSecret, cfg.CookieSecure, cfg.MaxSSEConns)
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      handler.Routes(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
		BaseContext:  func(net.Listener) context.Context { return ctx },
	}

	go func() {
		<-ctx.Done()
		slog.Info("shutting down, draining active streams")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("shutdown error", "err", err)
		}
		logStore.Flush()
	}()

	slog.Info("chowkidar server listening", "port", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

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
			slog.Info("loaded env", "path", p)
			return
		}
	}
	if os.Getenv("SERVER_PORT") == "" {
		slog.Error("no .env file found and no env vars set", "tried", candidates)
		os.Exit(1)
	}
	slog.Info("no .env file found, relying on process environment")
}
