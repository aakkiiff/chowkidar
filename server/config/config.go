package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port                    string
	JWTSecret               string
	DBPath                  string
	RetentionDaysContainers int
	RawRetentionMinutes     int
	LogDir                  string
	LogRetentionDays        int
	LogMaxFileMB            int
	LogMaxRotations         int
	// Optional tuning — have safe defaults, not required in .env.
	CookieSecure bool
	MaxSSEConns  int
}

// Load reads every required env var. Missing or invalid values fail fast —
// no silent defaults. Caller is expected to have loaded .env first.
func Load() (*Config, error) {
	var missing []string
	get := func(key string) string {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			missing = append(missing, key)
		}
		return v
	}
	getInt := func(key string) int {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			missing = append(missing, key)
			return 0
		}
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			missing = append(missing, key+" (must be positive integer)")
			return 0
		}
		return n
	}

	rawRetMin := 2
	if v := strings.TrimSpace(os.Getenv("RAW_RETENTION_MINUTES")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			rawRetMin = n
		}
	}

	cookieSecure := false
	if v := strings.TrimSpace(os.Getenv("COOKIE_SECURE")); v == "true" || v == "1" {
		cookieSecure = true
	}

	maxSSE := 200
	if v := strings.TrimSpace(os.Getenv("MAX_SSE_CONNS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxSSE = n
		}
	}

	cfg := &Config{
		Port:                    get("SERVER_PORT"),
		JWTSecret:               get("JWT_SECRET"),
		DBPath:                  get("DB_PATH"),
		RetentionDaysContainers: getInt("RETENTION_DAYS_CONTAINERS"),
		LogDir:                  get("LOG_DIR"),
		LogRetentionDays:        getInt("LOG_RETENTION_DAYS"),
		LogMaxFileMB:            getInt("LOG_MAX_FILE_MB"),
		LogMaxRotations:         getInt("LOG_MAX_ROTATIONS"),
		RawRetentionMinutes:     rawRetMin,
		CookieSecure:            cookieSecure,
		MaxSSEConns:             maxSSE,
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}
	if len(cfg.JWTSecret) < 32 {
		return nil, fmt.Errorf("JWT_SECRET must be at least 32 characters")
	}
	return cfg, nil
}
