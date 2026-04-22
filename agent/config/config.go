package config

import (
	"log"
	"os"
	"time"
)

type Config struct {
	ServerURL       string
	Identity        string
	Token           string
	CollectInterval time.Duration
}

func Load() *Config {
	return &Config{
		ServerURL:       getEnv("SERVER_URL", "http://localhost:8080"),
		Identity:        getEnv("AGENT_IDENTITY", ""),
		Token:           getEnv("AGENT_TOKEN", ""),
		CollectInterval: getEnvDuration("COLLECT_INTERVAL", 10*time.Second),
	}
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
		log.Printf("invalid %s=%q, using default %v", key, v, fallback)
		return fallback
	}
	return d
}
