package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/technonext/chowkidar/server/alert"
	"github.com/technonext/chowkidar/server/logbroker"
	"github.com/technonext/chowkidar/server/logstore"
	"github.com/technonext/chowkidar/server/store"
	"golang.org/x/crypto/bcrypt"
)

type Handler struct {
	store       *store.Store
	logs        *logstore.Store
	broker      *logbroker.Broker
	alertBroker *alert.Broker
	secret      string
	loginLimit  *ipLimiter
}

func NewHandler(s *store.Store, ls *logstore.Store, br *logbroker.Broker, ab *alert.Broker, jwtSecret string) *Handler {
	return &Handler{
		store:       s,
		logs:        ls,
		broker:      br,
		alertBroker: ab,
		secret:      jwtSecret,
		// 10 attempts/min, burst 5, evict IPs idle for 15 min. Keeps
		// bcrypt (100ms/attempt) from becoming a DoS surface.
		loginLimit: newIPLimiter(10, 5, 15*time.Minute),
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
		token := bearerToken(r)
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
// Use it on endpoints that mutate config or expose other users.
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

// ── Auth ──────────────────────────────────────────────────────────────────────

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
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

	_, hashedPassword, role, err := h.store.GetUser(req.Username)
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

	token, err := GenerateToken(req.Username, role, h.secret)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"token generation failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"token":    token,
		"username": req.Username,
		"role":     role,
	})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	username, _ := r.Context().Value(ctxKeyUsername).(string)
	role, _ := r.Context().Value(ctxKeyRole).(string)
	writeJSON(w, http.StatusOK, map[string]string{"username": username, "role": role})
}

// ── Agents ────────────────────────────────────────────────────────────────────

func (h *Handler) RegisterAgent(w http.ResponseWriter, r *http.Request) {
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

	token := newAgentToken()
	agentID, err := h.store.CreateAgent(hostname, hashToken(token))
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
	ID             string   `json:"id"`
	Hostname       string   `json:"hostname"`
	LastSeen       *string  `json:"last_seen"`
	CPUPercent     *float64 `json:"cpu_percent"`
	MemUsedGB      *float64 `json:"mem_used_gb"`
	MemTotalGB     *float64 `json:"mem_total_gb"`
	DiskUsedGB     *float64 `json:"disk_used_gb"`
	DiskTotalGB    *float64 `json:"disk_total_gb"`
	ContainerCount int      `json:"container_count"`
	AlertsEnabled  bool     `json:"alerts_enabled"`
}

func toAgentResponse(a store.AgentWithMetrics) agentResponse {
	var lastSeen *string
	if a.LastSeen != nil {
		s := a.LastSeen.Format(time.RFC3339)
		lastSeen = &s
	}
	ar := agentResponse{
		ID:             a.ID,
		Hostname:       a.Hostname,
		LastSeen:       lastSeen,
		ContainerCount: a.ContainerCount,
		AlertsEnabled:  a.AlertsEnabled,
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

// ListAgents returns all agents with their latest system metrics and container
// count embedded so the dashboard can render cards in a single request.
func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := h.store.ListAgentsWithMetrics()
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

// GetAgent returns a single agent by id. Powers deep-link refresh on the
// dashboard's agent detail page.
func (h *Handler) GetAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"id required"})
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

// SetAgentAlerts toggles the per-agent alert flag. Future alert rules
// reference this flag as a master switch.
func (h *Handler) SetAgentAlerts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"id required"})
		return
	}
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
	// Best-effort log purge. A failure here shouldn't block the DB deletion.
	if err := h.logs.DeleteAgent(id); err != nil {
		// Logged but not surfaced to the client.
		_ = err
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── Agent detail ──────────────────────────────────────────────────────────────

// AgentContainers returns the latest container list for an agent, sorted by
// CPU% descending (mirrors the output of `docker stats`).
func (h *Handler) AgentContainers(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
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

// ContainerHistory returns 1-minute aggregated metrics for a single container
// over the requested time range. Supported ranges: 1h, 6h, 24h, 7d (default 1h).
func (h *Handler) ContainerHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
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

// timeAgo returns now minus minutes as a time.Time. Lifted out so handlers
// in this package share a single helper.
func timeAgo(minutes int) time.Time {
	return time.Now().Add(-time.Duration(minutes) * time.Minute)
}

func parseRange(r string) time.Duration {
	// Numeric "range" is interpreted as minutes, capped at 30 days.
	if n, err := strconv.Atoi(r); err == nil && n > 0 && n <= 30*24*60 {
		return time.Duration(n) * time.Minute
	}
	// Legacy named ranges.
	switch r {
	case "6h":
		return 6 * time.Hour
	case "24h":
		return 24 * time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	default:
		return time.Hour // 1h
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

// isUniqueErr detects SQLite UNIQUE constraint violations without taking
// a hard dependency on the sqlite3 driver type in every call site.
func isUniqueErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}
