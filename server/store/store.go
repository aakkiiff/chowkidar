package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db           *sql.DB
	rawRetention time.Duration
}

// New opens (or creates) the SQLite database and runs schema migrations.
// It creates the parent directory if it does not exist.
func New(dbPath string, rawRetention time.Duration) (*Store, error) {
	if rawRetention <= 0 {
		rawRetention = defaultRawRetention
	}
	if dir := filepath.Dir(dbPath); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}
	// modernc.org/sqlite takes PRAGMAs via repeated `_pragma=` query params.
	// journal_mode=WAL + busy_timeout match the prior mattn DSN behaviour;
	// synchronous=NORMAL and temp_store=MEMORY are safe perf wins under WAL.
	dsn := dbPath +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(5000)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=temp_store(MEMORY)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// WAL mode allows concurrent readers alongside one writer.
	// _busy_timeout handles write-write contention at the SQLite level.
	// A pool of 10 lets the dashboard and agent reports proceed concurrently.
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)

	s := &Store{db: db, rawRetention: rawRetention}
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
			role       TEXT NOT NULL DEFAULT 'admin',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS agents (
			id             TEXT PRIMARY KEY,
			hostname       TEXT NOT NULL,
			token_hash     TEXT NOT NULL,
			last_seen      DATETIME,
			alerts_enabled INTEGER NOT NULL DEFAULT 0,
			created_at     DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		// Hostname is the human identifier shown on the dashboard; keep it unique.
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_agents_hostname ON agents(hostname)`,

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

		// Drop legacy system aggregates table if present — system is live-only now.
		`DROP TABLE IF EXISTS system_metrics_1m`,

		// 1-minute aggregates — only for containers. System metrics are live-only.
		// ts_minute is a Unix epoch truncated to 60-second boundaries.
		`CREATE TABLE IF NOT EXISTS container_metrics_1m (
			agent_id       TEXT NOT NULL,
			container_name TEXT NOT NULL,
			ts_minute      INTEGER NOT NULL,
			cpu_percent    REAL,
			mem_used_mb    REAL,
			mem_limit_mb   REAL,
			PRIMARY KEY (agent_id, container_name, ts_minute)
		)`,

		// HTTP endpoints monitored by the central prober. One row per agent
		// is the natural grouping — endpoints are dashboard-scoped to the
		// agent that "owns" them, even though probes run server-side today.
		`CREATE TABLE IF NOT EXISTS endpoints (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			agent_id        TEXT NOT NULL,
			name            TEXT NOT NULL,
			url             TEXT NOT NULL,
			alert_on_down   INTEGER NOT NULL DEFAULT 0,
			created_at      DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_endpoints_agent ON endpoints(agent_id)`,

		// One row per probe. Bounded by retention so the table never grows
		// unbounded; the prober prunes anything older than 7 days hourly.
		`CREATE TABLE IF NOT EXISTS endpoint_probes (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			endpoint_id     INTEGER NOT NULL,
			probed_at       DATETIME NOT NULL,
			status_code     INTEGER NOT NULL,
			latency_ms      INTEGER NOT NULL,
			ok              INTEGER NOT NULL,
			error           TEXT,
			cert_not_after  DATETIME
		)`,
		`CREATE INDEX IF NOT EXISTS idx_probes_endpoint_time ON endpoint_probes(endpoint_id, probed_at DESC)`,

		// One row per outage. probe_count + last_* refresh as the same incident
		// continues. Retention is days-of-history controlled, while the per-probe
		// table only holds 48h. This is the long-range source of truth for the
		// uptime gantt + KPI tiles.
		`CREATE TABLE IF NOT EXISTS endpoint_incidents (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			endpoint_id  INTEGER NOT NULL,
			started_at   DATETIME NOT NULL,
			ended_at     DATETIME,
			last_status  INTEGER,
			last_error   TEXT,
			probe_count  INTEGER NOT NULL DEFAULT 1
		)`,
		`CREATE INDEX IF NOT EXISTS idx_inc_ep_time ON endpoint_incidents(endpoint_id, started_at DESC)`,

		// Named webhook URLs. The name is the human handle referenced when
		// configuring per-agent alerts later; the URL is where alerts fire.
		`CREATE TABLE IF NOT EXISTS webhooks (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			name       TEXT UNIQUE NOT NULL,
			url        TEXT NOT NULL,
			type       TEXT NOT NULL DEFAULT 'discord',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		// Global key/value settings. Used for cross-agent tunables like
		// alert sustain + resend-cooldown windows.
		`CREATE TABLE IF NOT EXISTS app_settings (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,

		// One alert-rule row per agent. Thresholds are whole-percent integers
		// for simplicity. webhook_id is nullable so the rule can be saved even
		// if no webhook is selected yet, but no firing happens without one.
		`CREATE TABLE IF NOT EXISTS alert_rules (
			agent_id                TEXT PRIMARY KEY,
			cpu_enabled             INTEGER NOT NULL DEFAULT 0,
			cpu_threshold           INTEGER NOT NULL DEFAULT 80,
			mem_enabled             INTEGER NOT NULL DEFAULT 0,
			mem_threshold           INTEGER NOT NULL DEFAULT 85,
			disk_enabled            INTEGER NOT NULL DEFAULT 0,
			disk_threshold          INTEGER NOT NULL DEFAULT 90,
			ctr_down_enabled        INTEGER NOT NULL DEFAULT 0,
			ctr_cpu_enabled         INTEGER NOT NULL DEFAULT 0,
			ctr_cpu_threshold_mcore INTEGER NOT NULL DEFAULT 800,
			ctr_mem_enabled         INTEGER NOT NULL DEFAULT 0,
			ctr_mem_threshold       INTEGER NOT NULL DEFAULT 85,
			endpoint_down_enabled   INTEGER NOT NULL DEFAULT 0,
			ssl_alert_enabled       INTEGER NOT NULL DEFAULT 0,
			agent_down_enabled      INTEGER NOT NULL DEFAULT 0,
			webhook_id              INTEGER,
			updated_at              DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

			// Per-agent read permissions for developer users.
			`CREATE TABLE IF NOT EXISTS user_agent_perms (
				user_id  INTEGER NOT NULL,
				agent_id TEXT    NOT NULL,
				PRIMARY KEY (user_id, agent_id),
				FOREIGN KEY (user_id)  REFERENCES users(id)  ON DELETE CASCADE,
				FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
			)`,
	}

	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	// Post-commit additive migrations for databases created before a column
	// existed. SQLite has no "ADD COLUMN IF NOT EXISTS", so ignore the
	// duplicate-column error.
	for _, alter := range []string{
		`ALTER TABLE agents ADD COLUMN alerts_enabled INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE webhooks ADD COLUMN type TEXT NOT NULL DEFAULT 'discord'`,
		`ALTER TABLE alert_rules ADD COLUMN ctr_down_enabled INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE alert_rules ADD COLUMN ctr_cpu_enabled INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE alert_rules ADD COLUMN ctr_cpu_threshold_mcore INTEGER NOT NULL DEFAULT 800`,
		`ALTER TABLE alert_rules ADD COLUMN ctr_mem_enabled INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE alert_rules ADD COLUMN ctr_mem_threshold INTEGER NOT NULL DEFAULT 85`,
		`ALTER TABLE endpoints ADD COLUMN alert_on_down INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE alert_rules ADD COLUMN endpoint_down_enabled INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE alert_rules ADD COLUMN agent_down_enabled INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE alert_rules ADD COLUMN ssl_alert_enabled INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE endpoint_probes ADD COLUMN cert_not_after DATETIME`,
		`ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'admin'`,
		`ALTER TABLE container_metrics ADD COLUMN restart_count INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE container_metrics ADD COLUMN started_at TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE container_metrics ADD COLUMN net_rx_mb REAL NOT NULL DEFAULT 0`,
		`ALTER TABLE container_metrics ADD COLUMN net_tx_mb REAL NOT NULL DEFAULT 0`,
	} {
		if _, err := s.db.Exec(alter); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("migrate alter: %w", err)
		}
	}
		// Grant existing developers access to all existing agents so they don't
		// lose visibility on upgrade. Safe to re-run (INSERT OR IGNORE).
		s.db.Exec(`INSERT OR IGNORE INTO user_agent_perms (user_id, agent_id)
			SELECT u.id, a.id FROM users u CROSS JOIN agents a WHERE u.role = 'developer'`)

	return nil
}

// ── Users ─────────────────────────────────────────────────────────────────────

// CreateUser inserts the admin user on first run and updates the password on
// every subsequent restart so the DB always reflects the current ADMIN_PASSWORD.
// The bootstrap account is always 'admin' role.
func (s *Store) CreateUser(username, hashedPassword string) error {
	_, err := s.db.Exec(
		`INSERT INTO users (username, password, role) VALUES (?, ?, 'admin')
		 ON CONFLICT(username) DO UPDATE SET password = excluded.password, role = 'admin'`,
		username, hashedPassword,
	)
	return err
}

// GetUser returns the password hash + role for the named user.
func (s *Store) GetUser(username string) (id int, password, role string, err error) {
	err = s.db.QueryRow(
		`SELECT id, password, role FROM users WHERE username = ?`, username,
	).Scan(&id, &password, &role)
	return
}

// AppUser is the JSON-friendly view of a user (no password hash).
type AppUser struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	AgentIDs  []string  `json:"agent_ids,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) ListUsers() ([]AppUser, error) {
	rows, err := s.db.Query(`SELECT id, username, role, created_at FROM users ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppUser
	for rows.Next() {
		var u AppUser
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		if u.Role == "developer" {
			u.AgentIDs, _ = s.GetUserAgentPerms(u.ID)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CreateAppUser inserts a new user with the given role + bcrypt hash.
// Returns sqlite UNIQUE error when the username collides.
func (s *Store) CreateAppUser(username, hashedPassword, role string) (AppUser, error) {
	res, err := s.db.Exec(
		`INSERT INTO users (username, password, role) VALUES (?, ?, ?)`,
		username, hashedPassword, role,
	)
	if err != nil {
		return AppUser{}, err
	}
	id, _ := res.LastInsertId()
	var u AppUser
	err = s.db.QueryRow(
		`SELECT id, username, role, created_at FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt)
	return u, err
}

func (s *Store) DeleteAppUser(id int64) error {
	res, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) UpdateUserPassword(id int64, hashedPassword string) error {
	res, err := s.db.Exec(`UPDATE users SET password = ? WHERE id = ?`, hashedPassword, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

	// SetUserAgentPerms replaces the set of agent IDs a developer can see.
	// Pass an empty slice to revoke all access.
	func (s *Store) SetUserAgentPerms(userID int64, agentIDs []string) error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`DELETE FROM user_agent_perms WHERE user_id = ?`, userID); err != nil {
			return err
		}
		for _, aid := range agentIDs {
			if _, err := tx.Exec(`INSERT INTO user_agent_perms (user_id, agent_id) VALUES (?, ?)`, userID, aid); err != nil {
				return err
			}
		}
		return tx.Commit()
	}

	// GetUserAgentPerms returns the agent IDs a developer has access to.
	func (s *Store) GetUserAgentPerms(userID int64) ([]string, error) {
		rows, err := s.db.Query(`SELECT agent_id FROM user_agent_perms WHERE user_id = ?`, userID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		return ids, rows.Err()
	}

	// UserCanSeeAgent checks whether a developer has access to a specific agent.
	func (s *Store) UserCanSeeAgent(userID int64, agentID string) (bool, error) {
		var n int
		err := s.db.QueryRow(
			`SELECT COUNT(*) FROM user_agent_perms WHERE user_id = ? AND agent_id = ?`,
			userID, agentID,
		).Scan(&n)
		return n > 0, err
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

// AgentIDs returns every currently-registered agent id. Used by log-store
// orphan cleanup to drop any on-disk log dirs whose owning agent was deleted.
func (s *Store) AgentIDs() (map[string]struct{}, error) {
	rows, err := s.db.Query(`SELECT id FROM agents`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = struct{}{}
	}
	return out, rows.Err()
}

// RenameAgent updates an agent's hostname. Returns sql.ErrNoRows if the id
// does not exist. Callers should handle sqlite3 unique-constraint errors for
// collision with another agent.
func (s *Store) RenameAgent(id, hostname string) error {
	res, err := s.db.Exec(`UPDATE agents SET hostname = ? WHERE id = ?`, hostname, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteAgent removes an agent and all of its metrics in one transaction.
// Returns sql.ErrNoRows if the agent does not exist.
func (s *Store) DeleteAgent(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(`DELETE FROM agents WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}

	for _, stmt := range []string{
		`DELETE FROM system_metrics WHERE agent_id = ?`,
		`DELETE FROM container_metrics WHERE agent_id = ?`,
		`DELETE FROM container_metrics_1m WHERE agent_id = ?`,
	} {
		if _, err := tx.Exec(stmt, id); err != nil {
			return fmt.Errorf("cascade delete: %w", err)
		}
	}
	return tx.Commit()
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

// activeIssueCount returns a quick tally of things currently wrong for an agent:
// stopped containers + open endpoint incidents + containers breaching enabled thresholds.
// Agent-offline (+1) is added by the caller which already has last_seen.
func (s *Store) activeIssueCount(agentID string) int {
	total := 0

	// Stopped containers in the latest snapshot.
	var stopped int
	s.db.QueryRow(`
		SELECT COUNT(*) FROM container_metrics
		WHERE agent_id = ? AND timestamp = (
			SELECT MAX(timestamp) FROM container_metrics WHERE agent_id = ?
		) AND status != 'running'
	`, agentID, agentID).Scan(&stopped)
	total += stopped

	// Open endpoint incidents.
	var openInc int
	s.db.QueryRow(`
		SELECT COUNT(DISTINCT e.id) FROM endpoints e
		JOIN endpoint_incidents i ON i.endpoint_id = e.id
		WHERE e.agent_id = ? AND i.ended_at IS NULL
	`, agentID).Scan(&openInc)
	total += openInc

	// Containers over CPU threshold (only if rule is enabled).
	var ctrCPU int
	s.db.QueryRow(`
		SELECT COUNT(*) FROM container_metrics cm
		JOIN alert_rules ar ON ar.agent_id = cm.agent_id
		WHERE cm.agent_id = ?
		  AND cm.timestamp = (SELECT MAX(timestamp) FROM container_metrics WHERE cm.agent_id = ?)
		  AND ar.ctr_cpu_enabled = 1
		  AND (cm.cpu_percent * 10) > ar.ctr_cpu_threshold_mcore
	`, agentID, agentID).Scan(&ctrCPU)
	total += ctrCPU

	// Containers over mem threshold (only if rule is enabled).
	var ctrMem int
	s.db.QueryRow(`
		SELECT COUNT(*) FROM container_metrics cm
		JOIN alert_rules ar ON ar.agent_id = cm.agent_id
		WHERE cm.agent_id = ?
		  AND cm.timestamp = (SELECT MAX(timestamp) FROM container_metrics WHERE cm.agent_id = ?)
		  AND ar.ctr_mem_enabled = 1
		  AND cm.mem_limit_mb > 0
		  AND (cm.mem_used_mb / cm.mem_limit_mb * 100) > ar.ctr_mem_threshold
	`, agentID, agentID).Scan(&ctrMem)
	total += ctrMem

	return total
}

// ListAgentsWithMetrics returns agents with embedded metrics. Pass nil to
// return all agents (admin); pass a slice to filter to those IDs (developer).
func (s *Store) ListAgentsWithMetrics(allowed []string) ([]AgentWithMetrics, error) {
	q := `SELECT id, hostname, last_seen, alerts_enabled FROM agents`
	var args []any
	if allowed != nil {
		ph := make([]string, len(allowed))
		for i, id := range allowed {
			ph[i] = "?"
			args = append(args, id)
		}
		q += ` WHERE id IN (` + strings.Join(ph, ",") + `)`
	}
	q += ` ORDER BY last_seen DESC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []AgentWithMetrics
	for rows.Next() {
		var a AgentWithMetrics
		var enabled int
		if err := rows.Scan(&a.ID, &a.Hostname, &a.LastSeen, &enabled); err != nil {
			return nil, err
		}
		a.AlertsEnabled = enabled != 0
		a.System, _ = s.latestSystemMetrics(a.ID)
		a.ContainerCount, _ = s.latestContainerCount(a.ID)
		a.ActiveIssues = s.activeIssueCount(a.ID)
		if a.LastSeen != nil && time.Since(*a.LastSeen) > 35*time.Second {
			a.ActiveIssues++
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

// GetAgentWithMetrics returns one agent by id, with the same metric bundle
// as ListAgentsWithMetrics. Returns sql.ErrNoRows when the agent is missing.
func (s *Store) GetAgentWithMetrics(id string) (AgentWithMetrics, error) {
	var a AgentWithMetrics
	var enabled int
	err := s.db.QueryRow(
		`SELECT id, hostname, last_seen, alerts_enabled FROM agents WHERE id = ?`, id,
	).Scan(&a.ID, &a.Hostname, &a.LastSeen, &enabled)
	if err != nil {
		return AgentWithMetrics{}, err
	}
	a.AlertsEnabled = enabled != 0
	a.System, _ = s.latestSystemMetrics(a.ID)
	a.ContainerCount, _ = s.latestContainerCount(a.ID)
	a.ActiveIssues = s.activeIssueCount(a.ID)
	if a.LastSeen != nil && time.Since(*a.LastSeen) > 35*time.Second {
		a.ActiveIssues++
	}
	return a, nil
}

// ListAgentHostnames returns id→hostname for every agent. Used by the prober
// to build a per-cycle cache without fetching full metrics.
func (s *Store) ListAgentHostnames() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT id, hostname FROM agents`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, hostname string
		if err := rows.Scan(&id, &hostname); err != nil {
			return nil, err
		}
		out[id] = hostname
	}
	return out, rows.Err()
}

// ListAlertRules returns all alert rules keyed by agent_id. Missing rules use
// the default (all disabled). Used by the prober to batch the per-cycle lookup.
func (s *Store) ListAlertRules() (map[string]AlertRule, error) {
	rows, err := s.db.Query(`
		SELECT agent_id,
		       cpu_enabled, cpu_threshold,
		       mem_enabled, mem_threshold,
		       disk_enabled, disk_threshold,
		       ctr_down_enabled,
		       ctr_cpu_enabled, ctr_cpu_threshold_mcore,
		       ctr_mem_enabled, ctr_mem_threshold,
		       endpoint_down_enabled,
		       ssl_alert_enabled,
		       agent_down_enabled,
		       webhook_id
		FROM alert_rules`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]AlertRule{}
	for rows.Next() {
		var r AlertRule
		var cpuE, memE, diskE, ctrDownE, ctrCPUE, ctrMemE, epDownE, agentDownE, sslE int
		var webhookID sql.NullInt64
		if err := rows.Scan(&r.AgentID,
			&cpuE, &r.CPUThreshold,
			&memE, &r.MemThreshold,
			&diskE, &r.DiskThreshold,
			&ctrDownE,
			&ctrCPUE, &r.CtrCPUThresholdMCore,
			&ctrMemE, &r.CtrMemThreshold,
			&epDownE, &sslE, &agentDownE,
			&webhookID,
		); err != nil {
			return nil, err
		}
		r.CPUEnabled = cpuE != 0
		r.MemEnabled = memE != 0
		r.DiskEnabled = diskE != 0
		r.CtrDownEnabled = ctrDownE != 0
		r.CtrCPUEnabled = ctrCPUE != 0
		r.CtrMemEnabled = ctrMemE != 0
		r.EndpointDownEnabled = epDownE != 0
		r.SslAlertEnabled = sslE != 0
		r.AgentDownEnabled = agentDownE != 0
		if webhookID.Valid {
			id := webhookID.Int64
			r.WebhookID = &id
		}
		out[r.AgentID] = r
	}
	return out, rows.Err()
}

// SetAgentAlerts toggles the alert flag for an agent. Returns sql.ErrNoRows
// if the agent does not exist.
func (s *Store) SetAgentAlerts(id string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	res, err := s.db.Exec(`UPDATE agents SET alerts_enabled = ? WHERE id = ?`, v, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
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
		       cpu_percent, mem_used_mb, mem_limit_mb,
		       restart_count, started_at, net_rx_mb, net_tx_mb
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
		if err := rows.Scan(&c.ID, &c.Name, &c.Image, &c.Status,
			&c.CPUPercent, &c.MemUsedMB, &c.MemLimitMB,
			&c.RestartCount, &c.StartedAt, &c.NetRxMB, &c.NetTxMB); err != nil {
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
				 cpu_percent, mem_used_mb, mem_limit_mb,
				 restart_count, started_at, net_rx_mb, net_tx_mb)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return fmt.Errorf("prepare container stmt: %w", err)
		}
		defer stmt.Close()

		tsStr := ts.Format(time.RFC3339)
		for _, c := range containers {
			if _, err := stmt.Exec(agentID, c.ID, c.Name, c.Image, c.Status, tsStr,
				c.CPUPercent, c.MemUsedMB, c.MemLimitMB,
				c.RestartCount, c.StartedAt, c.NetRxMB, c.NetTxMB); err != nil {
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

// GetContainerHistoryByName returns 1-minute average metrics for a single
// container on the given agent, ordered oldest-first.
// Raw rows from the last 2 minutes are unioned in as live points so the graph
// has near-real-time data before the rollup job processes them.
func (s *Store) GetContainerHistoryByName(agentID, name string, since time.Time) ([]ContainerPoint, error) {
	rows, err := s.db.Query(`
		SELECT ts_minute, cpu_percent, mem_used_mb, mem_limit_mb
		FROM container_metrics_1m
		WHERE agent_id = ? AND container_name = ? AND ts_minute >= ?
		UNION ALL
		SELECT CAST(strftime('%s', timestamp) AS INTEGER),
		       cpu_percent, mem_used_mb, mem_limit_mb
		FROM container_metrics
		WHERE agent_id = ? AND container_name = ?
		  AND timestamp >= datetime('now', '-' || ? || ' seconds')
		ORDER BY 1 ASC
	`, agentID, name, since.Unix(), agentID, name, int(s.rawRetention.Seconds()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []ContainerPoint
	for rows.Next() {
		var tsUnix int64
		var p ContainerPoint
		if err := rows.Scan(&tsUnix, &p.CPUPercent, &p.MemUsedMB, &p.MemLimitMB); err != nil {
			return nil, err
		}
		p.Timestamp = time.Unix(tsUnix, 0).UTC()
		points = append(points, p)
	}
	return points, rows.Err()
}

// ── Webhooks ──────────────────────────────────────────────────────────────────

type Webhook struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) ListWebhooks() ([]Webhook, error) {
	rows, err := s.db.Query(`SELECT id, name, url, type, created_at FROM webhooks ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Webhook
	for rows.Next() {
		var w Webhook
		if err := rows.Scan(&w.ID, &w.Name, &w.URL, &w.Type, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) CreateWebhook(name, url, kind string) (Webhook, error) {
	res, err := s.db.Exec(`INSERT INTO webhooks (name, url, type) VALUES (?, ?, ?)`, name, url, kind)
	if err != nil {
		return Webhook{}, err
	}
	id, _ := res.LastInsertId()
	var w Webhook
	err = s.db.QueryRow(
		`SELECT id, name, url, type, created_at FROM webhooks WHERE id = ?`, id,
	).Scan(&w.ID, &w.Name, &w.URL, &w.Type, &w.CreatedAt)
	return w, err
}

func (s *Store) DeleteWebhook(id int64) error {
	res, err := s.db.Exec(`DELETE FROM webhooks WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ── App settings (global KV) ──────────────────────────────────────────────────

const (
	settingAlertSustainSec  = "alert_sustain_seconds"
	settingAlertResendSec   = "alert_resend_cooldown_seconds"
	defaultAlertSustainSec  = 60
	defaultAlertResendSec   = 0 // 0 = no resend (fire once per incident)
)

func (s *Store) getAppSetting(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (s *Store) setAppSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO app_settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

func (s *Store) getAppSettingInt(key string, fallback int) int {
	v, ok, err := s.getAppSetting(key)
	if err != nil || !ok {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

// AlertSustain returns how long a breach must be continuously observed
// before firing the first webhook.
func (s *Store) AlertSustain() time.Duration {
	return time.Duration(s.getAppSettingInt(settingAlertSustainSec, defaultAlertSustainSec)) * time.Second
}

// AlertResendCooldown returns how long to wait between successive fires
// while a breach remains unresolved. Zero disables re-firing.
func (s *Store) AlertResendCooldown() time.Duration {
	return time.Duration(s.getAppSettingInt(settingAlertResendSec, defaultAlertResendSec)) * time.Second
}

type AlertSettings struct {
	SustainSeconds        int `json:"sustain_seconds"`
	ResendCooldownSeconds int `json:"resend_cooldown_seconds"`
}

func (s *Store) GetAlertSettings() AlertSettings {
	return AlertSettings{
		SustainSeconds:        s.getAppSettingInt(settingAlertSustainSec, defaultAlertSustainSec),
		ResendCooldownSeconds: s.getAppSettingInt(settingAlertResendSec, defaultAlertResendSec),
	}
}

func (s *Store) SetAlertSettings(a AlertSettings) error {
	if err := s.setAppSetting(settingAlertSustainSec, strconv.Itoa(a.SustainSeconds)); err != nil {
		return err
	}
	return s.setAppSetting(settingAlertResendSec, strconv.Itoa(a.ResendCooldownSeconds))
}

// ── Endpoints ─────────────────────────────────────────────────────────────────

const (
	settingProbeIntervalSec   = "endpoint_probe_interval_seconds"
	settingIncidentRetentionD = "endpoint_incident_retention_days"
	defaultProbeIntervalSec   = 60
	// Per-probe rows feed the heartbeat strip + 1h latency chart only.
	// Fixed 1h window — long-range uptime queries hit endpoint_incidents.
	endpointProbeRetention = time.Hour
	// Closed incidents older than this are pruned. Open incidents (ongoing
	// outages) are never pruned regardless of age.
	defaultIncidentRetentionDays = 30
)

type Endpoint struct {
	ID          int64     `json:"id"`
	AgentID     string    `json:"agent_id"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	AlertOnDown bool      `json:"alert_on_down"`
	CreatedAt   time.Time `json:"created_at"`
}

type EndpointProbe struct {
	ID         int64      `json:"id"`
	EndpointID int64      `json:"endpoint_id"`
	ProbedAt   time.Time  `json:"probed_at"`
	StatusCode int        `json:"status_code"`
	LatencyMS  int        `json:"latency_ms"`
	OK         bool       `json:"ok"`
	Error      string     `json:"error,omitempty"`
	// Server cert NotAfter from this probe's TLS handshake. nil for plain http.
	CertNotAfter *time.Time `json:"cert_not_after,omitempty"`
}

// EndpointWithStats is what the dashboard renders: definition + the latest
// probe result rolled up so the list view has at-a-glance status.
type EndpointWithStats struct {
	Endpoint
	LastProbedAt     *time.Time `json:"last_probed_at"`
	LastStatusCode   *int       `json:"last_status_code"`
	LastLatencyMS    *int       `json:"last_latency_ms"`
	LastOK           *bool      `json:"last_ok"`
	LastError        string     `json:"last_error,omitempty"`
	LastCertNotAfter *time.Time `json:"last_cert_not_after,omitempty"`
}

func (s *Store) ListEndpoints(agentID string) ([]EndpointWithStats, error) {
	rows, err := s.db.Query(`
		SELECT e.id, e.agent_id, e.name, e.url, e.alert_on_down, e.created_at,
		       p.probed_at, p.status_code, p.latency_ms, p.ok, p.error, p.cert_not_after
		FROM endpoints e
		LEFT JOIN (
			SELECT ep.endpoint_id, ep.probed_at, ep.status_code, ep.latency_ms,
			       ep.ok, ep.error, ep.cert_not_after
			FROM endpoint_probes ep
			JOIN (
				SELECT endpoint_id, MAX(probed_at) AS m
				FROM endpoint_probes GROUP BY endpoint_id
			) latest ON latest.endpoint_id = ep.endpoint_id AND latest.m = ep.probed_at
		) p ON p.endpoint_id = e.id
		WHERE e.agent_id = ?
		ORDER BY e.created_at ASC
	`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EndpointWithStats
	for rows.Next() {
		var e EndpointWithStats
		var (
			alertOnDown int
			pAt         sql.NullTime
			pCode       sql.NullInt64
			pLat        sql.NullInt64
			pOK         sql.NullInt64
			pErr        sql.NullString
			pCert       sql.NullTime
		)
		if err := rows.Scan(
			&e.ID, &e.AgentID, &e.Name, &e.URL, &alertOnDown, &e.CreatedAt,
			&pAt, &pCode, &pLat, &pOK, &pErr, &pCert,
		); err != nil {
			return nil, err
		}
		e.AlertOnDown = alertOnDown != 0
		if pCert.Valid {
			t := pCert.Time
			e.LastCertNotAfter = &t
		}
		if pAt.Valid {
			t := pAt.Time
			e.LastProbedAt = &t
		}
		if pCode.Valid {
			c := int(pCode.Int64)
			e.LastStatusCode = &c
		}
		if pLat.Valid {
			l := int(pLat.Int64)
			e.LastLatencyMS = &l
		}
		if pOK.Valid {
			b := pOK.Int64 != 0
			e.LastOK = &b
		}
		if pErr.Valid {
			e.LastError = pErr.String
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) GetEndpoint(id int64) (Endpoint, error) {
	var e Endpoint
	var alertOnDown int
	err := s.db.QueryRow(
		`SELECT id, agent_id, name, url, alert_on_down, created_at FROM endpoints WHERE id = ?`, id,
	).Scan(&e.ID, &e.AgentID, &e.Name, &e.URL, &alertOnDown, &e.CreatedAt)
	if err == nil {
		e.AlertOnDown = alertOnDown != 0
	}
	return e, err
}

func (s *Store) CreateEndpoint(agentID, name, url string) (Endpoint, error) {
	res, err := s.db.Exec(
		`INSERT INTO endpoints (agent_id, name, url) VALUES (?, ?, ?)`,
		agentID, name, url,
	)
	if err != nil {
		return Endpoint{}, err
	}
	id, _ := res.LastInsertId()
	return s.GetEndpoint(id)
}

// UpdateEndpoint changes the name and URL of an existing endpoint. Returns
// sql.ErrNoRows when the id does not exist. Existing probe history is
// preserved — it remains pinned to the endpoint id, not the URL.
func (s *Store) UpdateEndpoint(id int64, name, url string) (Endpoint, error) {
	res, err := s.db.Exec(
		`UPDATE endpoints SET name = ?, url = ? WHERE id = ?`,
		name, url, id,
	)
	if err != nil {
		return Endpoint{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Endpoint{}, sql.ErrNoRows
	}
	return s.GetEndpoint(id)
}

func (s *Store) DeleteEndpoint(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM endpoint_probes WHERE endpoint_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`DELETE FROM endpoints WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

// AllEndpoints powers the prober loop; returns every configured endpoint
// regardless of agent.
func (s *Store) AllEndpoints() ([]Endpoint, error) {
	rows, err := s.db.Query(`SELECT id, agent_id, name, url, alert_on_down, created_at FROM endpoints`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Endpoint
	for rows.Next() {
		var e Endpoint
		var alertOnDown int
		if err := rows.Scan(&e.ID, &e.AgentID, &e.Name, &e.URL, &alertOnDown, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.AlertOnDown = alertOnDown != 0
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetEndpointAlert toggles the down-alert flag on a single endpoint.
func (s *Store) SetEndpointAlert(id int64, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	res, err := s.db.Exec(`UPDATE endpoints SET alert_on_down = ? WHERE id = ?`, v, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) RecordProbe(p EndpointProbe) error {
	okI := 0
	if p.OK {
		okI = 1
	}
	var cert any
	if p.CertNotAfter != nil {
		cert = p.CertNotAfter.UTC()
	}
	_, err := s.db.Exec(
		`INSERT INTO endpoint_probes
			(endpoint_id, probed_at, status_code, latency_ms, ok, error, cert_not_after)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.EndpointID, p.ProbedAt.UTC(), p.StatusCode, p.LatencyMS, okI, p.Error, cert,
	)
	return err
}

// GetEndpointProbes returns probes for a single endpoint ordered oldest→newest
// within the last `since` window. Powers the heartbeat strip + latency chart.
func (s *Store) GetEndpointProbes(endpointID int64, since time.Time) ([]EndpointProbe, error) {
	rows, err := s.db.Query(`
		SELECT id, endpoint_id, probed_at, status_code, latency_ms, ok, error, cert_not_after
		FROM endpoint_probes
		WHERE endpoint_id = ? AND probed_at >= ?
		ORDER BY probed_at ASC
	`, endpointID, since.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EndpointProbe
	for rows.Next() {
		var p EndpointProbe
		var okI int
		var errStr sql.NullString
		var cert sql.NullTime
		if err := rows.Scan(
			&p.ID, &p.EndpointID, &p.ProbedAt, &p.StatusCode, &p.LatencyMS, &okI, &errStr, &cert,
		); err != nil {
			return nil, err
		}
		p.OK = okI != 0
		if errStr.Valid {
			p.Error = errStr.String
		}
		if cert.Valid {
			t := cert.Time
			p.CertNotAfter = &t
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PruneEndpointProbes drops probe rows older than the retention window.
func (s *Store) PruneEndpointProbes() error {
	cutoff := time.Now().Add(-endpointProbeRetention)
	_, err := s.db.Exec(`DELETE FROM endpoint_probes WHERE probed_at < ?`, cutoff.UTC())
	return err
}

// ── Endpoint incidents ────────────────────────────────────────────────────────

type EndpointIncident struct {
	ID          int64      `json:"id"`
	EndpointID  int64      `json:"endpoint_id"`
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
	LastStatus  int        `json:"last_status"`
	LastError   string     `json:"last_error,omitempty"`
	ProbeCount  int        `json:"probe_count"`
	DurationS   int64      `json:"duration_s"`
}

// OpenIncident inserts a new ongoing outage row. Caller has already determined
// (via the prober's transition state machine) that this is a new ok→fail flip.
func (s *Store) OpenIncident(endpointID int64, at time.Time, status int, errStr string) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO endpoint_incidents (endpoint_id, started_at, last_status, last_error, probe_count)
		 VALUES (?, ?, ?, ?, 1)`,
		endpointID, at.UTC(), status, errStr,
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// BumpIncident records that the same outage is still ongoing. Called when a
// fail→fail probe arrives. Refreshes last_status / last_error and increments
// the probe counter.
func (s *Store) BumpIncident(id int64, status int, errStr string) error {
	_, err := s.db.Exec(
		`UPDATE endpoint_incidents
		 SET last_status = ?, last_error = ?, probe_count = probe_count + 1
		 WHERE id = ? AND ended_at IS NULL`,
		status, errStr, id,
	)
	return err
}

// CloseIncident sets ended_at on the latest open row for the endpoint. No-op
// if no open row exists (safe for first probe after server restart).
func (s *Store) CloseIncident(endpointID int64, at time.Time) error {
	_, err := s.db.Exec(
		`UPDATE endpoint_incidents
		 SET ended_at = ?
		 WHERE id = (
		   SELECT id FROM endpoint_incidents
		   WHERE endpoint_id = ? AND ended_at IS NULL
		   ORDER BY started_at DESC LIMIT 1
		 )`,
		at.UTC(), endpointID,
	)
	return err
}

// LatestOpenIncident returns the most recent ongoing incident for an endpoint,
// or 0 + sql.ErrNoRows if none. Used by the prober on startup to re-attach
// to a row left open by a previous process.
func (s *Store) LatestOpenIncident(endpointID int64) (int64, error) {
	var id int64
	err := s.db.QueryRow(
		`SELECT id FROM endpoint_incidents
		 WHERE endpoint_id = ? AND ended_at IS NULL
		 ORDER BY started_at DESC LIMIT 1`,
		endpointID,
	).Scan(&id)
	return id, err
}

// ListIncidents returns rows that overlap [since, now]. Includes ongoing
// incidents whose started_at is older than `since` so the gantt strip can
// clamp them at the range edge.
func (s *Store) ListIncidents(endpointID int64, since time.Time) ([]EndpointIncident, error) {
	rows, err := s.db.Query(
		`SELECT id, endpoint_id, started_at, ended_at, last_status, last_error, probe_count
		 FROM endpoint_incidents
		 WHERE endpoint_id = ?
		   AND (ended_at IS NULL OR ended_at >= ?)
		 ORDER BY started_at DESC`,
		endpointID, since.UTC(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EndpointIncident
	now := time.Now().UTC()
	for rows.Next() {
		var inc EndpointIncident
		var ended sql.NullTime
		var status sql.NullInt64
		var errStr sql.NullString
		if err := rows.Scan(&inc.ID, &inc.EndpointID, &inc.StartedAt, &ended,
			&status, &errStr, &inc.ProbeCount); err != nil {
			return nil, err
		}
		if ended.Valid {
			t := ended.Time
			inc.EndedAt = &t
			inc.DurationS = int64(t.Sub(inc.StartedAt).Seconds())
		} else {
			inc.DurationS = int64(now.Sub(inc.StartedAt).Seconds())
		}
		if status.Valid {
			inc.LastStatus = int(status.Int64)
		}
		if errStr.Valid {
			inc.LastError = errStr.String
		}
		out = append(out, inc)
	}
	return out, rows.Err()
}

// UptimeStats summarises uptime % and incident counters over a window. Open
// incidents are clamped to [start, now] so the math survives ongoing outages.
type UptimeStats struct {
	RangeStart    time.Time `json:"range_start"`
	RangeEnd      time.Time `json:"range_end"`
	TotalSeconds  int64     `json:"total_seconds"`
	DownSeconds   int64     `json:"down_seconds"`
	Percent       float64   `json:"percent"`
	IncidentCount int       `json:"incident_count"`
	MTTRSeconds   int64     `json:"mttr_seconds"`
	LongestSeconds int64    `json:"longest_seconds"`
}

func (s *Store) ComputeUptime(endpointID int64, start, end time.Time) (UptimeStats, error) {
	stats := UptimeStats{
		RangeStart:   start.UTC(),
		RangeEnd:     end.UTC(),
		TotalSeconds: int64(end.Sub(start).Seconds()),
	}
	if stats.TotalSeconds <= 0 {
		stats.Percent = 100
		return stats, nil
	}

	rows, err := s.db.Query(
		`SELECT started_at, ended_at FROM endpoint_incidents
		 WHERE endpoint_id = ?
		   AND (ended_at IS NULL OR ended_at >= ?)
		   AND started_at <= ?`,
		endpointID, start.UTC(), end.UTC(),
	)
	if err != nil {
		return stats, err
	}
	defer rows.Close()

	var totalDown, longest, sumClosed int64
	var closedN int
	for rows.Next() {
		var startedAt time.Time
		var ended sql.NullTime
		if err := rows.Scan(&startedAt, &ended); err != nil {
			return stats, err
		}
		windowStart := startedAt
		if windowStart.Before(start) {
			windowStart = start
		}
		var windowEnd time.Time
		if ended.Valid {
			windowEnd = ended.Time
			closedN++
			d := int64(ended.Time.Sub(startedAt).Seconds())
			sumClosed += d
		} else {
			windowEnd = end
		}
		if windowEnd.After(end) {
			windowEnd = end
		}
		seg := int64(windowEnd.Sub(windowStart).Seconds())
		if seg < 0 {
			seg = 0
		}
		totalDown += seg
		if seg > longest {
			longest = seg
		}
		stats.IncidentCount++
	}
	stats.DownSeconds = totalDown
	stats.LongestSeconds = longest
	stats.Percent = 100 * float64(stats.TotalSeconds-totalDown) / float64(stats.TotalSeconds)
	if closedN > 0 {
		stats.MTTRSeconds = sumClosed / int64(closedN)
	}
	return stats, rows.Err()
}

// PruneEndpointIncidents drops closed incident rows older than the retention
// window. Open incidents (ended_at IS NULL) are kept regardless.
func (s *Store) PruneEndpointIncidents() error {
	days := s.getAppSettingInt(settingIncidentRetentionD, defaultIncidentRetentionDays)
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	_, err := s.db.Exec(
		`DELETE FROM endpoint_incidents
		 WHERE ended_at IS NOT NULL AND ended_at < ?`,
		cutoff.UTC(),
	)
	return err
}

// ProbeInterval returns the global polling cadence.
func (s *Store) ProbeInterval() time.Duration {
	return time.Duration(s.getAppSettingInt(settingProbeIntervalSec, defaultProbeIntervalSec)) * time.Second
}

func (s *Store) GetEndpointSettings() map[string]int {
	return map[string]int{
		"probe_interval_seconds":  s.getAppSettingInt(settingProbeIntervalSec, defaultProbeIntervalSec),
		"incident_retention_days": s.getAppSettingInt(settingIncidentRetentionD, defaultIncidentRetentionDays),
	}
}

func (s *Store) SetEndpointSettings(intervalSeconds, incidentRetentionDays int) error {
	if err := s.setAppSetting(settingProbeIntervalSec, strconv.Itoa(intervalSeconds)); err != nil {
		return err
	}
	return s.setAppSetting(settingIncidentRetentionD, strconv.Itoa(incidentRetentionDays))
}

// ── Alert rules ───────────────────────────────────────────────────────────────

type AlertRule struct {
	AgentID       string `json:"agent_id"`
	// System (host) metrics
	CPUEnabled    bool `json:"cpu_enabled"`
	CPUThreshold  int  `json:"cpu_threshold"`
	MemEnabled    bool `json:"mem_enabled"`
	MemThreshold  int  `json:"mem_threshold"`
	DiskEnabled   bool `json:"disk_enabled"`
	DiskThreshold int  `json:"disk_threshold"`
	// Container-level metrics. Apply to every container under this agent.
	CtrDownEnabled       bool `json:"ctr_down_enabled"`
	CtrCPUEnabled        bool `json:"ctr_cpu_enabled"`
	CtrCPUThresholdMCore int  `json:"ctr_cpu_threshold_mcore"` // 1000 mCore = 1 full core
	CtrMemEnabled        bool `json:"ctr_mem_enabled"`
	CtrMemThreshold      int  `json:"ctr_mem_threshold"` // % of mem_limit
	// Single master toggle: when true, every endpoint registered under
	// this agent participates in down/up alerts.
	EndpointDownEnabled bool `json:"endpoint_down_enabled"`
	// Fires when any HTTPS endpoint's certificate is within the global
	// SSL warning window (default 14 days).
	SslAlertEnabled bool `json:"ssl_alert_enabled"`
	// Fires when the agent stops reporting for longer than the global
	// alert sustain window (host-side network or process death).
	AgentDownEnabled bool   `json:"agent_down_enabled"`
	WebhookID        *int64 `json:"webhook_id"`
}

const (
	defaultCPUThreshold       = 80
	defaultMemThreshold       = 85
	defaultDiskThreshold      = 90
	defaultCtrCPUMCore        = 800
	defaultCtrMemThresholdPct = 85
)

func defaultAlertRule(agentID string) AlertRule {
	return AlertRule{
		AgentID:              agentID,
		CPUThreshold:         defaultCPUThreshold,
		MemThreshold:         defaultMemThreshold,
		DiskThreshold:        defaultDiskThreshold,
		CtrCPUThresholdMCore: defaultCtrCPUMCore,
		CtrMemThreshold:      defaultCtrMemThresholdPct,
	}
}

func (s *Store) GetAlertRule(agentID string) (AlertRule, error) {
	var r AlertRule
	var cpuE, memE, diskE, ctrDownE, ctrCPUE, ctrMemE, epDownE, agentDownE, sslE int
	var webhookID sql.NullInt64
	err := s.db.QueryRow(`
		SELECT agent_id,
		       cpu_enabled, cpu_threshold,
		       mem_enabled, mem_threshold,
		       disk_enabled, disk_threshold,
		       ctr_down_enabled,
		       ctr_cpu_enabled, ctr_cpu_threshold_mcore,
		       ctr_mem_enabled, ctr_mem_threshold,
		       endpoint_down_enabled,
		       ssl_alert_enabled,
		       agent_down_enabled,
		       webhook_id
		FROM alert_rules WHERE agent_id = ?`, agentID,
	).Scan(&r.AgentID,
		&cpuE, &r.CPUThreshold,
		&memE, &r.MemThreshold,
		&diskE, &r.DiskThreshold,
		&ctrDownE,
		&ctrCPUE, &r.CtrCPUThresholdMCore,
		&ctrMemE, &r.CtrMemThreshold,
		&epDownE,
		&sslE,
		&agentDownE,
		&webhookID)
	if err == sql.ErrNoRows {
		return defaultAlertRule(agentID), nil
	}
	if err != nil {
		return AlertRule{}, err
	}
	r.CPUEnabled = cpuE != 0
	r.MemEnabled = memE != 0
	r.DiskEnabled = diskE != 0
	r.CtrDownEnabled = ctrDownE != 0
	r.CtrCPUEnabled = ctrCPUE != 0
	r.CtrMemEnabled = ctrMemE != 0
	r.EndpointDownEnabled = epDownE != 0
	r.SslAlertEnabled = sslE != 0
	r.AgentDownEnabled = agentDownE != 0
	if webhookID.Valid {
		v := webhookID.Int64
		r.WebhookID = &v
	}
	return r, nil
}

func (s *Store) UpsertAlertRule(r AlertRule) error {
	toInt := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	var webhookArg any
	if r.WebhookID != nil {
		webhookArg = *r.WebhookID
	}
	_, err := s.db.Exec(`
		INSERT INTO alert_rules
			(agent_id,
			 cpu_enabled, cpu_threshold,
			 mem_enabled, mem_threshold,
			 disk_enabled, disk_threshold,
			 ctr_down_enabled,
			 ctr_cpu_enabled, ctr_cpu_threshold_mcore,
			 ctr_mem_enabled, ctr_mem_threshold,
			 endpoint_down_enabled,
			 ssl_alert_enabled,
			 agent_down_enabled,
			 webhook_id, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(agent_id) DO UPDATE SET
			cpu_enabled             = excluded.cpu_enabled,
			cpu_threshold           = excluded.cpu_threshold,
			mem_enabled             = excluded.mem_enabled,
			mem_threshold           = excluded.mem_threshold,
			disk_enabled            = excluded.disk_enabled,
			disk_threshold          = excluded.disk_threshold,
			ctr_down_enabled        = excluded.ctr_down_enabled,
			ctr_cpu_enabled         = excluded.ctr_cpu_enabled,
			ctr_cpu_threshold_mcore = excluded.ctr_cpu_threshold_mcore,
			ctr_mem_enabled         = excluded.ctr_mem_enabled,
			ctr_mem_threshold       = excluded.ctr_mem_threshold,
			endpoint_down_enabled   = excluded.endpoint_down_enabled,
			ssl_alert_enabled       = excluded.ssl_alert_enabled,
			agent_down_enabled      = excluded.agent_down_enabled,
			webhook_id              = excluded.webhook_id,
			updated_at              = CURRENT_TIMESTAMP`,
		r.AgentID,
		toInt(r.CPUEnabled), r.CPUThreshold,
		toInt(r.MemEnabled), r.MemThreshold,
		toInt(r.DiskEnabled), r.DiskThreshold,
		toInt(r.CtrDownEnabled),
		toInt(r.CtrCPUEnabled), r.CtrCPUThresholdMCore,
		toInt(r.CtrMemEnabled), r.CtrMemThreshold,
		toInt(r.EndpointDownEnabled),
		toInt(r.SslAlertEnabled),
		toInt(r.AgentDownEnabled),
		webhookArg,
	)
	return err
}

// EvaluationRow carries everything the alert evaluator needs in one row:
// the rule + webhook (URL + type) + latest system metrics + agent hostname.
type EvaluationRow struct {
	AgentID     string
	Hostname    string
	Rule        AlertRule
	WebhookURL  string
	WebhookType string
	CPUPercent  float64
	MemPercent  float64
	DiskPercent float64
	HasMetrics  bool
	LastSeen    *time.Time // nil if the agent has never reported
}

// ListEvaluationRows returns one row per agent that has alerts_enabled=1 AND
// at least one metric rule (host or container) enabled. Latest system_metrics
// is joined if present.
func (s *Store) ListEvaluationRows() ([]EvaluationRow, error) {
	rows, err := s.db.Query(`
		SELECT a.id, a.hostname, a.last_seen,
		       COALESCE(r.cpu_enabled, 0),  COALESCE(r.cpu_threshold, 80),
		       COALESCE(r.mem_enabled, 0),  COALESCE(r.mem_threshold, 85),
		       COALESCE(r.disk_enabled, 0), COALESCE(r.disk_threshold, 90),
		       COALESCE(r.ctr_down_enabled, 0),
		       COALESCE(r.ctr_cpu_enabled, 0),  COALESCE(r.ctr_cpu_threshold_mcore, 800),
		       COALESCE(r.ctr_mem_enabled, 0),  COALESCE(r.ctr_mem_threshold, 85),
		       COALESCE(r.agent_down_enabled, 0),
		       r.webhook_id, w.url, w.type
		FROM agents a
		LEFT JOIN alert_rules r ON r.agent_id = a.id
		LEFT JOIN webhooks w    ON w.id = r.webhook_id
		WHERE a.alerts_enabled = 1
		  AND (COALESCE(r.cpu_enabled, 0)         = 1
		    OR COALESCE(r.mem_enabled, 0)         = 1
		    OR COALESCE(r.disk_enabled, 0)        = 1
		    OR COALESCE(r.ctr_down_enabled, 0)    = 1
		    OR COALESCE(r.ctr_cpu_enabled, 0)     = 1
		    OR COALESCE(r.ctr_mem_enabled, 0)     = 1
		    OR COALESCE(r.agent_down_enabled, 0)  = 1)
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EvaluationRow
	for rows.Next() {
		var e EvaluationRow
		var cpuE, memE, diskE, ctrDownE, ctrCPUE, ctrMemE, agentDownE int
		var lastSeen sql.NullTime
		var webhookID sql.NullInt64
		var webhookURL, webhookType sql.NullString
		if err := rows.Scan(
			&e.AgentID, &e.Hostname, &lastSeen,
			&cpuE, &e.Rule.CPUThreshold,
			&memE, &e.Rule.MemThreshold,
			&diskE, &e.Rule.DiskThreshold,
			&ctrDownE,
			&ctrCPUE, &e.Rule.CtrCPUThresholdMCore,
			&ctrMemE, &e.Rule.CtrMemThreshold,
			&agentDownE,
			&webhookID, &webhookURL, &webhookType,
		); err != nil {
			return nil, err
		}
		e.Rule.AgentID = e.AgentID
		e.Rule.CPUEnabled = cpuE != 0
		e.Rule.MemEnabled = memE != 0
		e.Rule.DiskEnabled = diskE != 0
		e.Rule.CtrDownEnabled = ctrDownE != 0
		e.Rule.CtrCPUEnabled = ctrCPUE != 0
		e.Rule.CtrMemEnabled = ctrMemE != 0
		e.Rule.AgentDownEnabled = agentDownE != 0
		if lastSeen.Valid {
			t := lastSeen.Time
			e.LastSeen = &t
		}
		if webhookID.Valid {
			v := webhookID.Int64
			e.Rule.WebhookID = &v
		}
		if webhookURL.Valid {
			e.WebhookURL = webhookURL.String
		}
		if webhookType.Valid {
			e.WebhookType = webhookType.String
		}

		// Latest metrics lookup.
		if m, err := s.latestSystemMetrics(e.AgentID); err == nil && m != nil {
			e.HasMetrics = true
			e.CPUPercent = m.CPUPercent
			if m.MemTotalGB > 0 {
				e.MemPercent = m.MemUsedGB / m.MemTotalGB * 100
			}
			if m.DiskTotalGB > 0 {
				e.DiskPercent = m.DiskUsedGB / m.DiskTotalGB * 100
			}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// WebhookURL returns the URL for a webhook ID, or empty if missing.
func (s *Store) WebhookURL(id int64) (string, error) {
	var url string
	err := s.db.QueryRow(`SELECT url FROM webhooks WHERE id = ?`, id).Scan(&url)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return url, err
}

// defaultRawRetention is used when no value is provided via config.
const defaultRawRetention = 2 * time.Minute

// ── Rollup & Prune ────────────────────────────────────────────────────────────

// RollupAndPrune should be called every minute. It:
//  1. Aggregates raw container data older than 1 minute into 1m buckets (idempotent via INSERT OR REPLACE)
//  2. Prunes raw data older than 2 minutes (live cards only read the latest row)
//  3. Prunes container 1m aggregates older than the configured retention window
//
// System metrics are live-only — no aggregation, no long-term storage.
func (s *Store) RollupAndPrune(containerDays int) error {
	now := time.Now().UTC()
	rollupBefore := now.Add(-time.Minute).Format(time.RFC3339)
	rawCutoff := now.Add(-s.rawRetention).Format(time.RFC3339)
	ctrCutoff := now.AddDate(0, 0, -containerDays).Unix()

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Roll up container metrics into 1-minute buckets.
	// (CAST(strftime('%s',timestamp)/60)*60) floors the timestamp to the minute boundary.
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

	// Prune container 1m aggregates beyond retention window.
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
	AlertsEnabled  bool
	ActiveIssues   int `json:"active_issues"`
}

type SystemMetrics struct {
	CPUPercent  float64 `json:"cpu_percent"`
	MemTotalGB  float64 `json:"mem_total_gb"`
	MemUsedGB   float64 `json:"mem_used_gb"`
	DiskTotalGB float64 `json:"disk_total_gb"`
	DiskUsedGB  float64 `json:"disk_used_gb"`
}

type ContainerMetrics struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Image        string  `json:"image"`
	Status       string  `json:"status"`
	CPUPercent   float64 `json:"cpu_percent"`
	MemUsedMB    float64 `json:"mem_used_mb"`
	MemLimitMB   float64 `json:"mem_limit_mb"`
	RestartCount int     `json:"restart_count"`
	StartedAt    string  `json:"started_at"`
	NetRxMB      float64 `json:"net_rx_mb"`
	NetTxMB      float64 `json:"net_tx_mb"`
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
