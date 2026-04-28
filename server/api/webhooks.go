package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/technonext/chowkidar/server/store"
)

func (h *Handler) ListWebhooks(w http.ResponseWriter, r *http.Request) {
	hooks, err := h.store.ListWebhooks()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to list webhooks"})
		return
	}
	if hooks == nil {
		hooks = []store.Webhook{}
	}
	writeJSON(w, http.StatusOK, hooks)
}

// supportedWebhookTypes mirrors the formatters registered in server/alert.
// Keep in sync when a new provider is added.
var supportedWebhookTypes = map[string]struct{}{
	"discord": {},
}

func (h *Handler) CreateWebhook(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		URL  string `json:"url"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}

	name := strings.TrimSpace(req.Name)
	rawURL := strings.TrimSpace(req.URL)
	kind := strings.TrimSpace(strings.ToLower(req.Type))
	if name == "" || rawURL == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"name and url required"})
		return
	}
	if len(name) > 64 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"name too long (max 64)"})
		return
	}
	if kind == "" {
		kind = "discord"
	}
	if _, ok := supportedWebhookTypes[kind]; !ok {
		writeJSON(w, http.StatusBadRequest, errorResponse{"unsupported webhook type"})
		return
	}

	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		writeJSON(w, http.StatusBadRequest, errorResponse{"url must be a valid http(s) URL"})
		return
	}

	hook, err := h.store.CreateWebhook(name, rawURL, kind)
	if err != nil {
		if isUniqueErr(err) {
			writeJSON(w, http.StatusConflict, errorResponse{"name already in use"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to create webhook"})
		return
	}
	writeJSON(w, http.StatusOK, hook)
}

// ── Alert rules ───────────────────────────────────────────────────────────────

func (h *Handler) GetAlertRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"id required"})
		return
	}
	rule, err := h.store.GetAlertRule(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to fetch rule"})
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (h *Handler) UpsertAlertRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"id required"})
		return
	}
	var req store.AlertRule
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}
	req.AgentID = id

	// Sanity bounds: thresholds expressed as whole percent.
	clamp := func(v int, fallback int) int {
		if v < 1 || v > 100 {
			return fallback
		}
		return v
	}
	req.CPUThreshold = clamp(req.CPUThreshold, 80)
	req.MemThreshold = clamp(req.MemThreshold, 85)
	req.DiskThreshold = clamp(req.DiskThreshold, 90)

	if err := h.store.UpsertAlertRule(req); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to save rule"})
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// ── Global alert settings ─────────────────────────────────────────────────────

func (h *Handler) GetAlertSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.store.GetAlertSettings())
}

func (h *Handler) SetAlertSettings(w http.ResponseWriter, r *http.Request) {
	var req store.AlertSettings
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}
	// Keep sustain reasonable: min 5s, max 1h. Resend 0 disables.
	if req.SustainSeconds < 5 || req.SustainSeconds > 3600 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"sustain_seconds must be between 5 and 3600"})
		return
	}
	if req.ResendCooldownSeconds < 0 || req.ResendCooldownSeconds > 24*3600 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"resend_cooldown_seconds must be between 0 and 86400"})
		return
	}
	if err := h.store.SetAlertSettings(req); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to save settings"})
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) DeleteWebhook(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid id"})
		return
	}
	if err := h.store.DeleteWebhook(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, errorResponse{"webhook not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to delete webhook"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
