package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/technonext/chowkidar/server/store"
)

func (h *Handler) ListEndpoints(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"id required"})
		return
	}
	out, err := h.store.ListEndpoints(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to list endpoints"})
		return
	}
	if out == nil {
		out = []store.EndpointWithStats{}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) CreateEndpoint(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"id required"})
		return
	}
	var req struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}
	name := strings.TrimSpace(req.Name)
	rawURL := strings.TrimSpace(req.URL)
	if rawURL == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"url required"})
		return
	}
	if len(rawURL) > 2048 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"url too long (max 2048)"})
		return
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		writeJSON(w, http.StatusBadRequest, errorResponse{"url must be a valid http(s) URL"})
		return
	}
	if name == "" {
		name = u.Host
	}
	if len(name) > 128 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"name too long"})
		return
	}
	ep, err := h.store.CreateEndpoint(id, name, rawURL)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to create endpoint"})
		return
	}
	writeJSON(w, http.StatusOK, ep)
}

func (h *Handler) UpdateEndpoint(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid id"})
		return
	}
	var req struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}
	name := strings.TrimSpace(req.Name)
	rawURL := strings.TrimSpace(req.URL)
	if rawURL == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"url required"})
		return
	}
	if len(rawURL) > 2048 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"url too long (max 2048)"})
		return
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		writeJSON(w, http.StatusBadRequest, errorResponse{"url must be a valid http(s) URL"})
		return
	}
	if name == "" {
		name = u.Host
	}
	if len(name) > 128 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"name too long"})
		return
	}
	ep, err := h.store.UpdateEndpoint(id, name, rawURL)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, errorResponse{"endpoint not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to update endpoint"})
		return
	}
	writeJSON(w, http.StatusOK, ep)
}

// SetEndpointAlert toggles the per-endpoint alert-on-down flag.
func (h *Handler) SetEndpointAlert(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid id"})
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}
	if err := h.store.SetEndpointAlert(id, req.Enabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, errorResponse{"endpoint not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to update alert flag"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "alert_on_down": req.Enabled})
}

func (h *Handler) DeleteEndpoint(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid id"})
		return
	}
	if err := h.store.DeleteEndpoint(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, errorResponse{"endpoint not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to delete endpoint"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// EndpointProbes returns recent probes for one endpoint within the given
// minutes window (default 60, capped at 7 days).
func (h *Handler) EndpointProbes(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid id"})
		return
	}
	minutes := 60
	if v := r.URL.Query().Get("minutes"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 7*24*60 {
			minutes = n
		}
	}
	since := timeAgo(minutes)
	probes, err := h.store.GetEndpointProbes(id, since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to fetch probes"})
		return
	}
	if probes == nil {
		probes = []store.EndpointProbe{}
	}
	writeJSON(w, http.StatusOK, probes)
}

// EndpointIncidents returns outage windows for an endpoint within the last
// N minutes (default 24 h, capped at 30 d). Powers the uptime gantt strip
// + incident table on the endpoint detail page.
func (h *Handler) EndpointIncidents(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid id"})
		return
	}
	minutes := parseRangeMinutes(r.URL.Query().Get("range"), 24*60, 30*24*60)
	since := time.Now().Add(-time.Duration(minutes) * time.Minute)
	out, err := h.store.ListIncidents(id, since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to list incidents"})
		return
	}
	if out == nil {
		out = []store.EndpointIncident{}
	}
	writeJSON(w, http.StatusOK, out)
}

// EndpointUptime returns the uptime % + downtime totals over a window.
func (h *Handler) EndpointUptime(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid id"})
		return
	}
	minutes := parseRangeMinutes(r.URL.Query().Get("range"), 24*60, 30*24*60)
	end := time.Now()
	start := end.Add(-time.Duration(minutes) * time.Minute)
	stats, err := h.store.ComputeUptime(id, start, end)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to compute uptime"})
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// parseRangeMinutes accepts integer minutes or the shorthand 1h/1d/7d/10d/30d.
// Falls back to fallback on parse failure; caps at maxMinutes.
func parseRangeMinutes(raw string, fallback, maxMinutes int) int {
	if raw == "" {
		return fallback
	}
	if n, err := strconv.Atoi(raw); err == nil {
		if n <= 0 {
			return fallback
		}
		if n > maxMinutes {
			return maxMinutes
		}
		return n
	}
	multipliers := map[byte]int{
		'm': 1,
		'h': 60,
		'd': 60 * 24,
	}
	if len(raw) < 2 {
		return fallback
	}
	suffix := raw[len(raw)-1]
	mul, ok := multipliers[suffix]
	if !ok {
		return fallback
	}
	n, err := strconv.Atoi(raw[:len(raw)-1])
	if err != nil || n <= 0 {
		return fallback
	}
	res := n * mul
	if res > maxMinutes {
		res = maxMinutes
	}
	return res
}

func (h *Handler) GetEndpointSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.store.GetEndpointSettings())
}

func (h *Handler) SetEndpointSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProbeIntervalSeconds  int `json:"probe_interval_seconds"`
		IncidentRetentionDays int `json:"incident_retention_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}
	if req.ProbeIntervalSeconds < 10 || req.ProbeIntervalSeconds > 3600 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"probe_interval_seconds must be between 10 and 3600"})
		return
	}
	if req.IncidentRetentionDays < 1 || req.IncidentRetentionDays > 365 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"incident_retention_days must be between 1 and 365"})
		return
	}
	if err := h.store.SetEndpointSettings(req.ProbeIntervalSeconds, req.IncidentRetentionDays); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to save"})
		return
	}
	writeJSON(w, http.StatusOK, h.store.GetEndpointSettings())
}
