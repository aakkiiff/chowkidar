package api

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// RecentAlerts returns the latest persisted alert events. Admin only — same
// permission model as the SSE alert stream. `limit` defaults to 100, capped
// at 500 by the store layer.
func (h *Handler) RecentAlerts(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	events, err := h.store.RecentAlertEvents(limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to load alerts"})
		return
	}
	count, _ := h.store.UnseenAlertCount()
	writeJSON(w, http.StatusOK, map[string]any{
		"events": events,
		"unread": count,
	})
}

// MarkAlertsSeen flips seen_at on every currently-unseen row. Returns the
// number marked. Bell badge clears immediately after.
func (h *Handler) MarkAlertsSeen(w http.ResponseWriter, r *http.Request) {
	n, err := h.store.MarkAllAlertsSeen()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to mark alerts seen"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"marked": n})
}

// GetAlertRetention returns the configured retention window in days.
func (h *Handler) GetAlertRetention(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]int{
		"days": h.store.GetAlertRetentionDays(),
	})
}

// SetAlertRetention validates + persists the retention window. Days must be
// between 1 and 90; values outside that range yield 400.
func (h *Handler) SetAlertRetention(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	var req struct {
		Days int `json:"days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}
	if err := h.store.SetAlertRetentionDays(req.Days); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"days": req.Days})
}
