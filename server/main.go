package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/technonext/chowkidar/server/api"
	"github.com/technonext/chowkidar/server/config"
	"github.com/technonext/chowkidar/server/store"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	godotenv.Load()

	cfg := config.Load()

	db, err := store.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()

	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.AdminPass), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("hash admin password: %v", err)
	}
	if err := db.CreateUser(cfg.AdminUser, string(hash)); err != nil {
		log.Fatalf("create admin user: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	handler := api.NewHandler(db, cfg.JWTSecret)
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      handler.Routes(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		<-ctx.Done()
		log.Println("shutting down...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	log.Printf("chowkidar server listening on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}
