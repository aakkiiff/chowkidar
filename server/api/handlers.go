package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/technonext/chowkidar/server/alert"
	"github.com/technonext/chowkidar/server/logbroker"
	"github.com/technonext/chowkidar/server/logstore"
	"github.com/technonext/chowkidar/server/store"
	"golang.org/x/crypto/bcrypt"
)

type Handler struct {
	store      *store.Store
	logs       *logstore.Store
	broker     *logbroker.Broker
	alertBroker *alert.Broker
	secret     string
	loginLimit *ipLimiter

	cookieSecure     bool
	maxSSEConns      int
	sseConns         atomic.Int64
	setupLimit       *ipLimiter
	agentReportLimit *ipLimiter
	agentIngestLimit *ipLimiter
}

func NewHandler(s *store.Store, ls *logstore.Store, br *logbroker.Broker, ab *alert.Broker, jwtSecret string, cookieSecure bool, maxSSEConns int) *Handler {
	return &Handler{
		store:       s,
		logs:        ls,
		broker:      br,
		alertBroker: ab,
		secret:      jwtSecret,
		cookieSecure: cookieSecure,
		maxSSEConns:  maxSSEConns,
		// 10 attempts/min, burst 5, evict IPs idle for 15 min.
		loginLimit: newIPLimiter(10, 5, 15*time.Minute),
		// Setup: very tight — 5/min, burst 3. One-time flow.
		setupLimit: newIPLimiter(5, 3, 15*time.Minute),
		// Per-agent-token rate limiters. Keyed by token hash, not IP.
		// Report: generous — 30/min covers 10s intervals with headroom.
		agentReportLimit: newIPLimiter(30, 5, 15*time.Minute),
		// IngestLogs: shipper batches by 200ms tick OR 8KB flush. Busy
		// hosts with 50+ chatty containers can saturate the 8KB threshold
		// many times per second — 100/sec sustained handles ~800 KB/s of
		// log throughput per agent. Token auth + 1MB body cap remain the
		// primary abuse defenses; this limiter just bounds the worst case.
		agentIngestLimit: newIPLimiter(6000, 200, 15*time.Minute),
	}
}

type ctxKey string

const (
	ctxKeyUsername ctxKey = "username"
	ctxKeyRole     ctxKey = "role"
)

// ── Middleware ────────────────────────────────────────────────────────────────

func (h *Handler) requireJWT(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Cookie takes precedence (browser clients); Authorization header is
		// the fallback for API/CLI clients and agent tokens won't collide here
		// since agent endpoints bypass requireJWT entirely.
		token := ""
		if c, err := r.Cookie("chowkidar_token"); err == nil && c.Value != "" {
			token = c.Value
		}
		if token == "" {
			token = bearerToken(r)
		}
		if token == "" {
			writeJSON(w, http.StatusUnauthorized, errorResponse{"missing token"})
			return
		}
		claims, err := ValidateToken(token, h.secret)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, errorResponse{"unauthorized"})
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeyUsername, claims.Username)
		ctx = context.WithValue(ctx, ctxKeyRole, claims.Role)
		next(w, r.WithContext(ctx))
	}
}

// requireAdmin wraps requireJWT and rejects non-admin requests with 403.
func (h *Handler) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return h.requireJWT(func(w http.ResponseWriter, r *http.Request) {
		role, _ := r.Context().Value(ctxKeyRole).(string)
		if role != RoleAdmin {
			writeJSON(w, http.StatusForbidden, errorResponse{"admin role required"})
			return
		}
		next(w, r)
	})
}

// sseGuard rejects new SSE connections when the global cap is reached.
// Call at the top of SSE handlers; the returned bool indicates whether to
// proceed (true) or abort (false, 503 already written).
func (h *Handler) sseGuard(w http.ResponseWriter) bool {
	if h.sseConns.Load() >= int64(h.maxSSEConns) {
		writeJSON(w, http.StatusServiceUnavailable, errorResponse{"too many concurrent streams"})
		return false
	}
	h.sseConns.Add(1)
	return true
}

func (h *Handler) sseRelease() { h.sseConns.Add(-1) }

// ── Auth ──────────────────────────────────────────────────────────────────────

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}
	if req.Username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"username and password required"})
		return
	}

	userID, hashedPassword, role, err := h.store.GetUser(req.Username)
	if err != nil {
		// Constant-time dummy compare prevents username enumeration via timing.
		bcrypt.CompareHashAndPassword([]byte("$2a$10$placeholder"), []byte(req.Password))
		writeJSON(w, http.StatusUnauthorized, errorResponse{"invalid credentials"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(req.Password)); err != nil {
		writeJSON(w, http.StatusUnauthorized, errorResponse{"invalid credentials"})
		return
	}

	// Record last login. Single UPDATE in WAL — sub-millisecond, no need to
	// fire-and-forget in a goroutine (would leak past test cleanup + drain
	// DB pool under load). Failure is logged but not propagated so auth
	// never breaks because of an audit-trail glitch.
	if err := h.store.TouchLastLogin(userID); err != nil {
		slog.Warn("touch last_login failed", "user_id", userID, "err", err)
	}

	token, err := GenerateToken(req.Username, role, h.secret)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"token generation failed"})
		return
	}

	// Set httpOnly cookie — JS cannot read this token, mitigating XSS token theft.
	http.SetCookie(w, &http.Cookie{
		Name:     "chowkidar_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(SessionTTL.Seconds()), // matches JWT expiry (5h)
		Secure:   h.cookieSecure,
	})
	writeJSON(w, http.StatusOK, map[string]string{
		"username": req.Username,
		"role":     role,
	})
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "chowkidar_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
		Secure:   h.cookieSecure,
	})
	w.WriteHeader(http.StatusNoContent)
}

// ── First-boot setup ──────────────────────────────────────────────────────────

// SetupStatus returns whether initial setup is still needed.
// Public endpoint — safe to call before any users exist.
func (h *Handler) SetupStatus(w http.ResponseWriter, r *http.Request) {
	has, err := h.store.HasUsers()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"db error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"setup_needed": !has})
}

// Setup creates the first admin user. Only works when no users exist.
func (h *Handler) Setup(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	has, err := h.store.HasUsers()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"db error"})
		return
	}
	if has {
		writeJSON(w, http.StatusForbidden, errorResponse{"setup already completed"})
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}
	if l := len(req.Password); l < 12 || l > 72 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"password must be 12–72 characters"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"internal error"})
		return
	}
	if _, err := h.store.CreateAppUser("admin", string(hash), RoleAdmin); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to create admin user"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"username": "admin"})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	username, _ := r.Context().Value(ctxKeyUsername).(string)
	role, _ := r.Context().Value(ctxKeyRole).(string)
	writeJSON(w, http.StatusOK, map[string]string{"username": username, "role": role})
}

// ── Agents ────────────────────────────────────────────────────────────────────

func (h *Handler) RegisterAgent(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	var req struct {
		Hostname  string `json:"hostname"`
		ProjectID int64  `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}
	hostname := strings.TrimSpace(req.Hostname)
	if hostname == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"hostname required"})
		return
	}
	if len(hostname) > 128 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"hostname too long (max 128)"})
		return
	}
	if req.ProjectID <= 0 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"project_id required"})
		return
	}
	// Verify project exists before creating the agent (avoids dangling FK).
	if _, err := h.store.GetProject(req.ProjectID); err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusBadRequest, errorResponse{"project not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to verify project"})
		return
	}

	token := newAgentToken()
	agentID, err := h.store.CreateAgent(hostname, hashToken(token), req.ProjectID)
	if err != nil {
		if isUniqueErr(err) {
			writeJSON(w, http.StatusConflict, errorResponse{"hostname already in use"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to create agent"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"agent_id": agentID, "token": token})
}

// RenameAgent updates an agent's hostname. Returns 409 on collision with
// another agent's existing hostname.
func (h *Handler) RenameAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"id required"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	var req struct {
		Hostname string `json:"hostname"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}
	hostname := strings.TrimSpace(req.Hostname)
	if hostname == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"hostname required"})
		return
	}
	if len(hostname) > 128 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"hostname too long (max 128)"})
		return
	}
	if err := h.store.RenameAgent(id, hostname); err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, errorResponse{"agent not found"})
			return
		}
		if isUniqueErr(err) {
			writeJSON(w, http.StatusConflict, errorResponse{"hostname already in use"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to rename agent"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "hostname": hostname})
}

// agentResponse is the JSON shape consumed by the dashboard. Kept identical
// for list + single-agent endpoints so the frontend has one Agent type.
type agentResponse struct {
	ID                 string   `json:"id"`
	Hostname           string   `json:"hostname"`
	LastSeen           *string  `json:"last_seen"`
	CPUPercent         *float64 `json:"cpu_percent"`
	MemUsedGB          *float64 `json:"mem_used_gb"`
	MemTotalGB         *float64 `json:"mem_total_gb"`
	DiskUsedGB         *float64 `json:"disk_used_gb"`
	DiskTotalGB        *float64 `json:"disk_total_gb"`
	ContainerCount     int      `json:"container_count"`
	AlertsEnabled      bool     `json:"alerts_enabled"`
	ActiveIssues       int      `json:"active_issues"`
	ProjectID          int64    `json:"project_id"`
	ProjectName        string   `json:"project_name"`
	ProjectEnvironment string   `json:"project_environment"`
}

func toAgentResponse(a store.AgentWithMetrics) agentResponse {
	var lastSeen *string
	if a.LastSeen != nil {
		s := a.LastSeen.Format(time.RFC3339)
		lastSeen = &s
	}
	ar := agentResponse{
		ID:                 a.ID,
		Hostname:           a.Hostname,
		LastSeen:           lastSeen,
		ContainerCount:     a.ContainerCount,
		AlertsEnabled:      a.AlertsEnabled,
		ActiveIssues:       a.ActiveIssues,
		ProjectID:          a.ProjectID,
		ProjectName:        a.ProjectName,
		ProjectEnvironment: a.ProjectEnvironment,
	}
	if a.System != nil {
		ar.CPUPercent = &a.System.CPUPercent
		ar.MemUsedGB = &a.System.MemUsedGB
		ar.MemTotalGB = &a.System.MemTotalGB
		ar.DiskUsedGB = &a.System.DiskUsedGB
		ar.DiskTotalGB = &a.System.DiskTotalGB
	}
	return ar
}

// canSeeAgent returns true for admins (always) or developers with a matching
// permission row. Returns false on any lookup error — fail-closed.
func (h *Handler) canSeeAgent(r *http.Request, agentID string) bool {
	role, _ := r.Context().Value(ctxKeyRole).(string)
	if role == RoleAdmin {
		return true
	}
	username, _ := r.Context().Value(ctxKeyUsername).(string)
	userID, _, _, err := h.store.GetUser(username)
	if err != nil {
		return false
	}
	ok, _ := h.store.UserCanSeeAgent(int64(userID), agentID)
	return ok
}

// userAllowedAgents returns the agent IDs a developer can see, or nil for admins.
func (h *Handler) userAllowedAgents(r *http.Request) ([]string, error) {
	role, _ := r.Context().Value(ctxKeyRole).(string)
	if role == RoleAdmin {
		return nil, nil
	}
	username, _ := r.Context().Value(ctxKeyUsername).(string)
	userID, _, _, err := h.store.GetUser(username)
	if err != nil {
		return nil, err
	}
	return h.store.GetUserAgentPerms(int64(userID))
}

// ListAgents returns all agents with their latest system metrics and container
// count embedded so the dashboard can render cards in a single request.
func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	allowed, err := h.userAllowedAgents(r)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to resolve permissions"})
		return
	}
	agents, err := h.store.ListAgentsWithMetrics(allowed)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to list agents"})
		return
	}
	resp := make([]agentResponse, 0, len(agents))
	for _, a := range agents {
		resp = append(resp, toAgentResponse(a))
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetAgent returns a single agent by id.
func (h *Handler) GetAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"id required"})
		return
	}
	if !h.canSeeAgent(r, id) {
		writeJSON(w, http.StatusNotFound, errorResponse{"agent not found"})
		return
	}
	a, err := h.store.GetAgentWithMetrics(id)
	if err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, errorResponse{"agent not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to fetch agent"})
		return
	}
	writeJSON(w, http.StatusOK, toAgentResponse(a))
}

// SetAgentAlerts toggles the per-agent alert flag.
func (h *Handler) SetAgentAlerts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"id required"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}
	if err := h.store.SetAgentAlerts(id, req.Enabled); err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, errorResponse{"agent not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to update alerts"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "alerts_enabled": req.Enabled})
}

// DeleteAgent removes the agent, its stored metrics, and its log directory.
func (h *Handler) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"id required"})
		return
	}
	if err := h.store.DeleteAgent(id); err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, errorResponse{"agent not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to delete agent"})
		return
	}
	if err := h.logs.DeleteAgent(id); err != nil {
		_ = err
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Agent detail ──────────────────────────────────────────────────────────────

func (h *Handler) AgentContainers(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !h.canSeeAgent(r, id) {
		writeJSON(w, http.StatusNotFound, errorResponse{"agent not found"})
		return
	}
	containers, err := h.store.GetLatestContainers(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to fetch containers"})
		return
	}
	if containers == nil {
		containers = []store.ContainerMetrics{}
	}
	writeJSON(w, http.StatusOK, containers)
}

func (h *Handler) ContainerHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !h.canSeeAgent(r, id) {
		writeJSON(w, http.StatusNotFound, errorResponse{"agent not found"})
		return
	}
	name := r.PathValue("name")
	since := time.Now().Add(-parseRange(r.URL.Query().Get("range")))

	points, err := h.store.GetContainerHistoryByName(id, name, since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to fetch container history"})
		return
	}
	if points == nil {
		points = []store.ContainerPoint{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"points": points})
}

func timeAgo(minutes int) time.Time {
	return time.Now().Add(-time.Duration(minutes) * time.Minute)
}

func parseRange(r string) time.Duration {
	if n, err := strconv.Atoi(r); err == nil && n > 0 && n <= 30*24*60 {
		return time.Duration(n) * time.Minute
	}
	switch r {
	case "6h":
		return 6 * time.Hour
	case "24h":
		return 24 * time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	default:
		return time.Hour
	}
}

// ── Report ────────────────────────────────────────────────────────────────────

func (h *Handler) Report(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, errorResponse{"missing token"})
		return
	}
	agentID, err := h.store.ValidateToken(hashToken(token))
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, errorResponse{"unauthorized"})
		return
	}

	// Cap body at 1 MB — prevents memory exhaustion from rogue agent tokens.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Timestamp  time.Time                `json:"timestamp"`
		System     store.SystemMetrics      `json:"system"`
		Containers []store.ContainerMetrics `json:"containers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}

	if err := h.store.SaveReport(agentID, req.Timestamp, req.System, req.Containers); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to save report"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── Health ────────────────────────────────────────────────────────────────────

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func bearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return auth[7:]
	}
	return ""
}

func newAgentToken() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return "agt_" + hex.EncodeToString(b)
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func isUniqueErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}
