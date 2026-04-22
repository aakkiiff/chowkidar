package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/technonext/chowkidar/server/logstore"
)

// IngestLogs consumes a long-lived NDJSON POST body from an agent. Each line
// is one logstore.Line. TCP backpressure throttles the agent if the server
// cannot keep up writing to disk.
func (h *Handler) IngestLogs(w http.ResponseWriter, r *http.Request) {
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

	scanner := bufio.NewScanner(r.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024) // allow up to 1 MB per line
	for scanner.Scan() {
		var l logstore.Line
		if err := json.Unmarshal(scanner.Bytes(), &l); err != nil {
			continue
		}
		if err := h.logs.Append(agentID, l); err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse{"append failed"})
			return
		}
		h.broker.Publish(agentID, l)
	}
	// scanner.Err() is expected on client disconnect — treat as success.
	w.WriteHeader(http.StatusOK)
}

// RecentLogs returns a JSON array of log lines for one container within the
// last N minutes (capped). This is the non-streaming path used when Live
// mode is off — the client polls explicitly via Refresh.
func (h *Handler) RecentLogs(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	name := r.PathValue("name")

	minutes := 5
	if v := r.URL.Query().Get("minutes"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 24*60 {
			minutes = n
		}
	}
	limit := 5000
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 20000 {
			limit = n
		}
	}

	past, err := h.logs.Tail(agentID, name, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to read logs"})
		return
	}

	cutoff := time.Now().Add(-time.Duration(minutes) * time.Minute)
	out := make([]logstore.Line, 0, len(past))
	for _, l := range past {
		if l.Timestamp.After(cutoff) {
			out = append(out, l)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// TailLogs streams log lines for one container over Server-Sent Events.
// Sends a tail-backfill of recent lines first, then live lines until the
// client disconnects.
func (h *Handler) TailLogs(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	name := r.PathValue("name")

	n := 100
	if v := r.URL.Query().Get("tail"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 && parsed <= 5000 {
			n = parsed
		}
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"streaming unsupported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // nginx: do not buffer

	// Backfill first.
	past, _ := h.logs.Tail(agentID, name, n)
	for _, l := range past {
		writeSSE(w, "log", l)
	}
	flusher.Flush()

	// Live stream.
	ch, unsub := h.broker.Subscribe(agentID, name, 256)
	defer unsub()

	// Heartbeat keeps proxies from killing idle connections.
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case l, ok := <-ch:
			if !ok {
				return
			}
			writeSSE(w, "log", l)
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, event string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
}
