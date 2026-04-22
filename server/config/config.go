package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port                    string
	JWTSecret               string
	DBPath                  string
	AdminUser               string
	AdminPass               string
	RetentionDaysContainers int
}

func Load() *Config {
	return &Config{
		Port:                    getEnv("SERVER_PORT", "8080"),
		JWTSecret:               getEnv("JWT_SECRET", "chowkidar-dev-secret"),
		DBPath:                  getEnv("DB_PATH", "./db/chowkidar.db"),
		AdminUser:               getEnv("ADMIN_USERNAME", "admin"),
		AdminPass:               getEnv("ADMIN_PASSWORD", "changeme"),
		RetentionDaysContainers: getEnvInt("RETENTION_DAYS_CONTAINERS", 7),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i > 0 {
			return i
		}
	}
	return fallback
}
