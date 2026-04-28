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
	AdminUser               string
	AdminPass               string
	RetentionDaysContainers int
	LogDir                  string
	LogRetentionDays        int
	LogMaxFileMB            int
	LogMaxRotations         int
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

	cfg := &Config{
		Port:                    get("SERVER_PORT"),
		JWTSecret:               get("JWT_SECRET"),
		DBPath:                  get("DB_PATH"),
		AdminUser:               get("ADMIN_USERNAME"),
		AdminPass:               get("ADMIN_PASSWORD"),
		RetentionDaysContainers: getInt("RETENTION_DAYS_CONTAINERS"),
		LogDir:                  get("LOG_DIR"),
		LogRetentionDays:        getInt("LOG_RETENTION_DAYS"),
		LogMaxFileMB:            getInt("LOG_MAX_FILE_MB"),
		LogMaxRotations:         getInt("LOG_MAX_ROTATIONS"),
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}
