package api

import "net/http"

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	// Public
	mux.HandleFunc("GET /api/v1/health", h.Health)
	mux.HandleFunc("POST /api/v1/auth/login", h.loginLimit.middleware(h.Login))

	// Auth + read-only views available to every signed-in user.
	mux.HandleFunc("GET /api/v1/auth/me", h.requireJWT(h.Me))
	mux.HandleFunc("GET /api/v1/agents", h.requireJWT(h.ListAgents))
	mux.HandleFunc("GET /api/v1/agents/{id}", h.requireJWT(h.GetAgent))
	mux.HandleFunc("GET /api/v1/agents/{id}/containers", h.requireJWT(h.AgentContainers))
	mux.HandleFunc("GET /api/v1/agents/{id}/containers/{name}/history", h.requireJWT(h.ContainerHistory))
	mux.HandleFunc("GET /api/v1/agents/{id}/containers/{name}/logs", h.requireJWT(h.RecentLogs))
	mux.HandleFunc("GET /api/v1/agents/{id}/containers/{name}/logs/tail", h.requireJWT(h.TailLogs))
	mux.HandleFunc("GET /api/v1/alerts/stream", h.requireAdmin(h.StreamAlerts))

	// Admin-only: agent CRUD + all configuration surfaces.
	mux.HandleFunc("POST /api/v1/agents/register", h.requireAdmin(h.RegisterAgent))
	mux.HandleFunc("PATCH /api/v1/agents/{id}", h.requireAdmin(h.RenameAgent))
	mux.HandleFunc("DELETE /api/v1/agents/{id}", h.requireAdmin(h.DeleteAgent))
	mux.HandleFunc("PATCH /api/v1/agents/{id}/alerts", h.requireAdmin(h.SetAgentAlerts))
	mux.HandleFunc("GET /api/v1/agents/{id}/alert-rule", h.requireAdmin(h.GetAlertRule))
	mux.HandleFunc("PUT /api/v1/agents/{id}/alert-rule", h.requireAdmin(h.UpsertAlertRule))

	// Endpoint monitoring — admin only.
	mux.HandleFunc("GET /api/v1/agents/{id}/endpoints", h.requireAdmin(h.ListEndpoints))
	mux.HandleFunc("POST /api/v1/agents/{id}/endpoints", h.requireAdmin(h.CreateEndpoint))
	mux.HandleFunc("PUT /api/v1/endpoints/{id}", h.requireAdmin(h.UpdateEndpoint))
	mux.HandleFunc("PATCH /api/v1/endpoints/{id}/alert", h.requireAdmin(h.SetEndpointAlert))
	mux.HandleFunc("DELETE /api/v1/endpoints/{id}", h.requireAdmin(h.DeleteEndpoint))
	mux.HandleFunc("GET /api/v1/endpoints/{id}/probes", h.requireAdmin(h.EndpointProbes))
	mux.HandleFunc("GET /api/v1/settings/endpoints", h.requireAdmin(h.GetEndpointSettings))
	mux.HandleFunc("PUT /api/v1/settings/endpoints", h.requireAdmin(h.SetEndpointSettings))

	// Webhooks — admin only.
	mux.HandleFunc("GET /api/v1/webhooks", h.requireAdmin(h.ListWebhooks))
	mux.HandleFunc("POST /api/v1/webhooks", h.requireAdmin(h.CreateWebhook))
	mux.HandleFunc("DELETE /api/v1/webhooks/{id}", h.requireAdmin(h.DeleteWebhook))

	// Global alert timing — admin only.
	mux.HandleFunc("GET /api/v1/settings/alerts", h.requireAdmin(h.GetAlertSettings))
	mux.HandleFunc("PUT /api/v1/settings/alerts", h.requireAdmin(h.SetAlertSettings))

	// User management — admin only.
	mux.HandleFunc("GET /api/v1/users", h.requireAdmin(h.ListUsers))
	mux.HandleFunc("POST /api/v1/users", h.requireAdmin(h.CreateUser))
	mux.HandleFunc("DELETE /api/v1/users/{id}", h.requireAdmin(h.DeleteUser))
	mux.HandleFunc("PUT /api/v1/users/{id}/password", h.requireAdmin(h.SetUserPassword))

	// Agent reporting — bearer token (not JWT).
	mux.HandleFunc("POST /api/v1/report", h.Report)
	mux.HandleFunc("POST /api/v1/logs/ingest", h.IngestLogs)

	return mux
}
