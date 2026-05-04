package config

import (
	"fmt"
	"log/slog"
	"os"
	"time"
)

type Config struct {
	ServerURL       string
	Identity        string
	Token           string
	CollectInterval time.Duration
	LogBatchMS      time.Duration
	LogBatchBytes   int
}

func Load() *Config {
	return &Config{
		ServerURL:       getEnv("SERVER_URL", "http://localhost:8080"),
		Identity:        getEnv("AGENT_IDENTITY", ""),
		Token:           getEnv("AGENT_TOKEN", ""),
		CollectInterval: getEnvDuration("COLLECT_INTERVAL", 10*time.Second),
		LogBatchMS:      getEnvDuration("LOG_BATCH_MS", 200*time.Millisecond),
		LogBatchBytes:   getEnvInt("LOG_BATCH_BYTES", 8192),
	}
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
		slog.Warn("invalid config value", "key", key, "value", v, "default", fallback)
		return fallback
	}
	return n
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		slog.Warn("invalid config value", "key", key, "value", v, "default", fallback)
		return fallback
	}
	return d
}
