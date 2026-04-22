package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/technonext/chowkidar/server/store"
	"golang.org/x/crypto/bcrypt"
)

type Handler struct {
	store  *store.Store
	secret string
}

func NewHandler(s *store.Store, jwtSecret string) *Handler {
	return &Handler{store: s, secret: jwtSecret}
}

type ctxKey string

const ctxKeyUsername ctxKey = "username"

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
		next(w, r.WithContext(ctx))
	}
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

	_, hashedPassword, err := h.store.GetUser(req.Username)
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

	token, err := GenerateToken(req.Username, h.secret)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"token generation failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token, "username": req.Username})
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	username := r.Context().Value(ctxKeyUsername).(string)
	writeJSON(w, http.StatusOK, map[string]string{"username": username})
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
	if req.Hostname == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"hostname required"})
		return
	}

	token := newAgentToken()
	agentID, err := h.store.CreateAgent(req.Hostname, hashToken(token))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to create agent"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"agent_id": agentID, "token": token})
}

// ListAgents returns all agents with their latest system metrics and container
// count embedded so the dashboard can render cards in a single request.
func (h *Handler) ListAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := h.store.ListAgentsWithMetrics()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to list agents"})
		return
	}

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
	}

	resp := make([]agentResponse, 0, len(agents))
	for _, a := range agents {
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
		}
		if a.System != nil {
			ar.CPUPercent = &a.System.CPUPercent
			ar.MemUsedGB = &a.System.MemUsedGB
			ar.MemTotalGB = &a.System.MemTotalGB
			ar.DiskUsedGB = &a.System.DiskUsedGB
			ar.DiskTotalGB = &a.System.DiskTotalGB
		}
		resp = append(resp, ar)
	}
	writeJSON(w, http.StatusOK, resp)
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

func parseRange(r string) time.Duration {
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
