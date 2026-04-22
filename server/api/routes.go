package api

import "net/http"

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	// Public
	mux.HandleFunc("GET /api/v1/health", h.Health)
	mux.HandleFunc("POST /api/v1/auth/login", h.Login)

	// Dashboard — requires JWT
	mux.HandleFunc("GET /api/v1/auth/me", h.requireJWT(h.Me))
	mux.HandleFunc("GET /api/v1/agents", h.requireJWT(h.ListAgents))
	mux.HandleFunc("POST /api/v1/agents/register", h.requireJWT(h.RegisterAgent))
	mux.HandleFunc("GET /api/v1/agents/{id}/history", h.requireJWT(h.AgentHistory))
	mux.HandleFunc("GET /api/v1/agents/{id}/containers", h.requireJWT(h.AgentContainers))

	// Agent reporting — bearer token (not JWT)
	mux.HandleFunc("POST /api/v1/report", h.Report)

	return mux
}
