package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db *sql.DB
}

// New opens (or creates) the SQLite database and runs schema migrations.
// It creates the parent directory if it does not exist.
func New(dbPath string) (*Store, error) {
	if dir := filepath.Dir(dbPath); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// WAL mode allows concurrent readers alongside one writer.
	// _busy_timeout handles write-write contention at the SQLite level.
	// A pool of 10 lets the dashboard and agent reports proceed concurrently.
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// migrate creates all tables in a single transaction.
func (s *Store) migrate() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			username   TEXT UNIQUE NOT NULL,
			password   TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS agents (
			id         TEXT PRIMARY KEY,
			hostname   TEXT NOT NULL,
			token_hash TEXT NOT NULL,
			last_seen  DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		// Raw metrics — kept for 2 minutes, pruned by RollupAndPrune.
		`CREATE TABLE IF NOT EXISTS system_metrics (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			agent_id      TEXT NOT NULL,
			timestamp     TEXT NOT NULL,
			cpu_percent   REAL,
			mem_total_gb  REAL,
			mem_used_gb   REAL,
			disk_total_gb REAL,
			disk_used_gb  REAL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sys_raw ON system_metrics(agent_id, timestamp)`,
		`CREATE TABLE IF NOT EXISTS container_metrics (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			agent_id       TEXT NOT NULL,
			container_id   TEXT NOT NULL,
			container_name TEXT,
			image          TEXT,
			status         TEXT,
			timestamp      TEXT NOT NULL,
			cpu_percent    REAL,
			mem_used_mb    REAL,
			mem_limit_mb   REAL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ctr_raw ON container_metrics(agent_id, timestamp)`,

		// 1-minute aggregates — kept for RETENTION_DAYS, queried for charts.
		// ts_minute is a Unix epoch truncated to 60-second boundaries.
		`CREATE TABLE IF NOT EXISTS system_metrics_1m (
			agent_id      TEXT NOT NULL,
			ts_minute     INTEGER NOT NULL,
			cpu_percent   REAL,
			mem_used_gb   REAL,
			mem_total_gb  REAL,
			disk_used_gb  REAL,
			disk_total_gb REAL,
			PRIMARY KEY (agent_id, ts_minute)
		)`,
		`CREATE TABLE IF NOT EXISTS container_metrics_1m (
			agent_id       TEXT NOT NULL,
			container_name TEXT NOT NULL,
			ts_minute      INTEGER NOT NULL,
			cpu_percent    REAL,
			mem_used_mb    REAL,
			mem_limit_mb   REAL,
			PRIMARY KEY (agent_id, container_name, ts_minute)
		)`,
	}

	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ── Users ─────────────────────────────────────────────────────────────────────

// CreateUser inserts the admin user on first run and updates the password on
// every subsequent restart so the DB always reflects the current ADMIN_PASSWORD.
func (s *Store) CreateUser(username, hashedPassword string) error {
	_, err := s.db.Exec(
		`INSERT INTO users (username, password) VALUES (?, ?)
		 ON CONFLICT(username) DO UPDATE SET password = excluded.password`,
		username, hashedPassword,
	)
	return err
}

func (s *Store) GetUser(username string) (id int, password string, err error) {
	err = s.db.QueryRow(
		`SELECT id, password FROM users WHERE username = ?`, username,
	).Scan(&id, &password)
	return
}

// ── Agents ────────────────────────────────────────────────────────────────────

func (s *Store) CreateAgent(hostname, tokenHash string) (string, error) {
	id := newID()
	_, err := s.db.Exec(
		`INSERT INTO agents (id, hostname, token_hash) VALUES (?, ?, ?)`,
		id, hostname, tokenHash,
	)
	return id, err
}

func (s *Store) ValidateToken(tokenHash string) (string, error) {
	var id string
	err := s.db.QueryRow(
		`SELECT id FROM agents WHERE token_hash = ?`, tokenHash,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("invalid token")
	}
	return id, nil
}

// ListAgentsWithMetrics returns all agents with their latest system metrics
// and current container count embedded — one query per agent for simplicity.
func (s *Store) ListAgentsWithMetrics() ([]AgentWithMetrics, error) {
	rows, err := s.db.Query(
		`SELECT id, hostname, last_seen FROM agents ORDER BY last_seen DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []AgentWithMetrics
	for rows.Next() {
		var a AgentWithMetrics
		if err := rows.Scan(&a.ID, &a.Hostname, &a.LastSeen); err != nil {
			return nil, err
		}
		a.System, _ = s.latestSystemMetrics(a.ID)
		a.ContainerCount, _ = s.latestContainerCount(a.ID)
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

func (s *Store) latestSystemMetrics(agentID string) (*SystemMetrics, error) {
	var m SystemMetrics
	err := s.db.QueryRow(`
		SELECT cpu_percent, mem_used_gb, mem_total_gb, disk_used_gb, disk_total_gb
		FROM system_metrics WHERE agent_id = ? ORDER BY timestamp DESC LIMIT 1
	`, agentID).Scan(&m.CPUPercent, &m.MemUsedGB, &m.MemTotalGB, &m.DiskUsedGB, &m.DiskTotalGB)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &m, err
}

func (s *Store) latestContainerCount(agentID string) (int, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM container_metrics
		WHERE agent_id = ? AND timestamp = (
			SELECT MAX(timestamp) FROM container_metrics WHERE agent_id = ?
		)
	`, agentID, agentID).Scan(&count)
	return count, err
}

// GetLatestContainers returns all containers from the agent's most recent report,
// sorted by CPU% descending (like docker stats).
func (s *Store) GetLatestContainers(agentID string) ([]ContainerMetrics, error) {
	rows, err := s.db.Query(`
		SELECT container_id, COALESCE(container_name, container_id), image, status,
		       cpu_percent, mem_used_mb, mem_limit_mb
		FROM container_metrics
		WHERE agent_id = ? AND timestamp = (
			SELECT MAX(timestamp) FROM container_metrics WHERE agent_id = ?
		)
		ORDER BY cpu_percent DESC
	`, agentID, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var containers []ContainerMetrics
	for rows.Next() {
		var c ContainerMetrics
		if err := rows.Scan(&c.ID, &c.Name, &c.Image, &c.Status, &c.CPUPercent, &c.MemUsedMB, &c.MemLimitMB); err != nil {
			return nil, err
		}
		containers = append(containers, c)
	}
	return containers, rows.Err()
}

// ── Reports ───────────────────────────────────────────────────────────────────

// SaveReport saves system + container metrics and updates last_seen atomically.
// Timestamps are truncated to second precision and stored as RFC3339 UTC strings
// so SQLite string comparison behaves correctly.
func (s *Store) SaveReport(agentID string, ts time.Time, sys SystemMetrics, containers []ContainerMetrics) error {
	// Normalise: UTC, second precision, consistent RFC3339 format for string comparison.
	ts = ts.UTC().Truncate(time.Second)

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		INSERT INTO system_metrics
			(agent_id, timestamp, cpu_percent, mem_used_gb, mem_total_gb, disk_used_gb, disk_total_gb)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
		agentID, ts.Format(time.RFC3339),
		sys.CPUPercent, sys.MemUsedGB, sys.MemTotalGB, sys.DiskUsedGB, sys.DiskTotalGB,
	); err != nil {
		return fmt.Errorf("insert system metrics: %w", err)
	}

	if len(containers) > 0 {
		stmt, err := tx.Prepare(`
			INSERT INTO container_metrics
				(agent_id, container_id, container_name, image, status, timestamp,
				 cpu_percent, mem_used_mb, mem_limit_mb)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return fmt.Errorf("prepare container stmt: %w", err)
		}
		defer stmt.Close()

		tsStr := ts.Format(time.RFC3339)
		for _, c := range containers {
			if _, err := stmt.Exec(agentID, c.ID, c.Name, c.Image, c.Status, tsStr,
				c.CPUPercent, c.MemUsedMB, c.MemLimitMB); err != nil {
				return fmt.Errorf("insert container metrics: %w", err)
			}
		}
	}

	if _, err := tx.Exec(
		`UPDATE agents SET last_seen = ? WHERE id = ?`, ts.Format(time.RFC3339), agentID,
	); err != nil {
		return fmt.Errorf("update last_seen: %w", err)
	}

	return tx.Commit()
}

// ── History ───────────────────────────────────────────────────────────────────

// GetSystemHistory returns 1-minute average system metrics for the given agent
// since the specified time, ordered oldest-first for chart rendering.
func (s *Store) GetSystemHistory(agentID string, since time.Time) ([]SystemPoint, error) {
	rows, err := s.db.Query(`
		SELECT ts_minute, cpu_percent, mem_used_gb, mem_total_gb, disk_used_gb, disk_total_gb
		FROM system_metrics_1m
		WHERE agent_id = ? AND ts_minute >= ?
		ORDER BY ts_minute ASC
	`, agentID, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []SystemPoint
	for rows.Next() {
		var tsUnix int64
		var p SystemPoint
		if err := rows.Scan(&tsUnix, &p.CPUPercent, &p.MemUsedGB, &p.MemTotalGB, &p.DiskUsedGB, &p.DiskTotalGB); err != nil {
			return nil, err
		}
		p.Timestamp = time.Unix(tsUnix, 0).UTC()
		points = append(points, p)
	}
	return points, rows.Err()
}

// GetContainerHistory returns 1-minute average container metrics grouped by
// container name, ordered oldest-first per container.
func (s *Store) GetContainerHistory(agentID string, since time.Time) (map[string][]ContainerPoint, error) {
	rows, err := s.db.Query(`
		SELECT container_name, ts_minute, cpu_percent, mem_used_mb, mem_limit_mb
		FROM container_metrics_1m
		WHERE agent_id = ? AND ts_minute >= ?
		ORDER BY container_name, ts_minute ASC
	`, agentID, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]ContainerPoint)
	for rows.Next() {
		var name string
		var tsUnix int64
		var p ContainerPoint
		if err := rows.Scan(&name, &tsUnix, &p.CPUPercent, &p.MemUsedMB, &p.MemLimitMB); err != nil {
			return nil, err
		}
		p.Timestamp = time.Unix(tsUnix, 0).UTC()
		result[name] = append(result[name], p)
	}
	return result, rows.Err()
}

// ── Rollup & Prune ────────────────────────────────────────────────────────────

// RollupAndPrune should be called every minute. It:
//  1. Aggregates raw data older than 1 minute into 1m buckets (idempotent via INSERT OR REPLACE)
//  2. Prunes raw data older than 2 minutes (live cards need only the latest row)
//  3. Prunes 1m data older than the configured retention windows
func (s *Store) RollupAndPrune(systemDays, containerDays int) error {
	now := time.Now().UTC()
	rollupBefore := now.Add(-time.Minute).Format(time.RFC3339)
	rawCutoff := now.Add(-2 * time.Minute).Format(time.RFC3339)
	sysCutoff := now.AddDate(0, 0, -systemDays).Unix()
	ctrCutoff := now.AddDate(0, 0, -containerDays).Unix()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Roll up system metrics into 1-minute buckets.
	// (CAST(strftime('%s',timestamp)/60)*60) floors the timestamp to the minute boundary.
	if _, err := tx.Exec(`
		INSERT OR REPLACE INTO system_metrics_1m
			(agent_id, ts_minute, cpu_percent, mem_used_gb, mem_total_gb, disk_used_gb, disk_total_gb)
		SELECT
			agent_id,
			(CAST(strftime('%s', timestamp) AS INTEGER) / 60) * 60,
			AVG(cpu_percent), AVG(mem_used_gb), AVG(mem_total_gb),
			AVG(disk_used_gb), AVG(disk_total_gb)
		FROM system_metrics
		WHERE timestamp < ?
		GROUP BY agent_id, (CAST(strftime('%s', timestamp) AS INTEGER) / 60) * 60
	`, rollupBefore); err != nil {
		return fmt.Errorf("rollup system: %w", err)
	}

	// Roll up container metrics into 1-minute buckets.
	if _, err := tx.Exec(`
		INSERT OR REPLACE INTO container_metrics_1m
			(agent_id, container_name, ts_minute, cpu_percent, mem_used_mb, mem_limit_mb)
		SELECT
			agent_id,
			COALESCE(container_name, container_id),
			(CAST(strftime('%s', timestamp) AS INTEGER) / 60) * 60,
			AVG(cpu_percent), AVG(mem_used_mb), AVG(mem_limit_mb)
		FROM container_metrics
		WHERE timestamp < ?
		GROUP BY agent_id,
		         COALESCE(container_name, container_id),
		         (CAST(strftime('%s', timestamp) AS INTEGER) / 60) * 60
	`, rollupBefore); err != nil {
		return fmt.Errorf("rollup containers: %w", err)
	}

	// Prune raw data (keep 2 minutes — live cards only read latest row).
	if _, err := tx.Exec(`DELETE FROM system_metrics WHERE timestamp < ?`, rawCutoff); err != nil {
		return fmt.Errorf("prune system raw: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM container_metrics WHERE timestamp < ?`, rawCutoff); err != nil {
		return fmt.Errorf("prune container raw: %w", err)
	}

	// Prune 1m aggregates beyond retention window.
	if _, err := tx.Exec(`DELETE FROM system_metrics_1m WHERE ts_minute < ?`, sysCutoff); err != nil {
		return fmt.Errorf("prune system 1m: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM container_metrics_1m WHERE ts_minute < ?`, ctrCutoff); err != nil {
		return fmt.Errorf("prune container 1m: %w", err)
	}

	return tx.Commit()
}

// ── Types ─────────────────────────────────────────────────────────────────────

type Agent struct {
	ID       string
	Hostname string
	LastSeen *time.Time
}

type AgentWithMetrics struct {
	Agent
	System         *SystemMetrics
	ContainerCount int
}

type SystemMetrics struct {
	CPUPercent  float64 `json:"cpu_percent"`
	MemTotalGB  float64 `json:"mem_total_gb"`
	MemUsedGB   float64 `json:"mem_used_gb"`
	DiskTotalGB float64 `json:"disk_total_gb"`
	DiskUsedGB  float64 `json:"disk_used_gb"`
}

type ContainerMetrics struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Image      string  `json:"image"`
	Status     string  `json:"status"`
	CPUPercent float64 `json:"cpu_percent"`
	MemUsedMB  float64 `json:"mem_used_mb"`
	MemLimitMB float64 `json:"mem_limit_mb"`
}

type SystemPoint struct {
	Timestamp   time.Time `json:"timestamp"`
	CPUPercent  float64   `json:"cpu_percent"`
	MemUsedGB   float64   `json:"mem_used_gb"`
	MemTotalGB  float64   `json:"mem_total_gb"`
	DiskUsedGB  float64   `json:"disk_used_gb"`
	DiskTotalGB float64   `json:"disk_total_gb"`
}

type ContainerPoint struct {
	Timestamp  time.Time `json:"timestamp"`
	CPUPercent float64   `json:"cpu_percent"`
	MemUsedMB  float64   `json:"mem_used_mb"`
	MemLimitMB float64   `json:"mem_limit_mb"`
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func newID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return "agt_" + hex.EncodeToString(b)
}
