package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/technonext/chowkidar/server/store"
)

const (
	maxProjectName = 64
	maxProjectEnv  = 32
)

// ListProjects returns projects visible to the caller. Admins see everything;
// developers see only projects that contain at least one agent they can access.
func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request) {
	role, _ := r.Context().Value(ctxKeyRole).(string)
	if role == RoleAdmin {
		projects, err := h.store.ListProjects()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to list projects"})
			return
		}
		writeJSON(w, http.StatusOK, projects)
		return
	}
	allowed, err := h.userAllowedAgents(r)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to resolve permissions"})
		return
	}
	projects, err := h.store.ListProjectsForAgents(allowed)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to list projects"})
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

// GetProject returns one project by id. Developers get 404 if they have no
// agent access in the project — same response as a missing project, to avoid
// leaking project existence.
func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProjectID(w, r)
	if !ok {
		return
	}
	p, err := h.store.GetProject(id)
	if err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, errorResponse{"project not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to fetch project"})
		return
	}
	role, _ := r.Context().Value(ctxKeyRole).(string)
	if role != RoleAdmin {
		allowed, err := h.userAllowedAgents(r)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to resolve permissions"})
			return
		}
		hasAccess, err := h.store.ProjectHasAnyOfAgents(id, allowed)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to check access"})
			return
		}
		if !hasAccess {
			writeJSON(w, http.StatusNotFound, errorResponse{"project not found"})
			return
		}
	}
	writeJSON(w, http.StatusOK, p)
}

// CreateProject creates a new project. Admin only.
func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	var req struct {
		Name        string `json:"name"`
		Environment string `json:"environment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}
	name := strings.TrimSpace(req.Name)
	env := strings.TrimSpace(req.Environment)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"name required"})
		return
	}
	if len(name) > maxProjectName {
		writeJSON(w, http.StatusBadRequest, errorResponse{"name too long (max 64)"})
		return
	}
	if len(env) > maxProjectEnv {
		writeJSON(w, http.StatusBadRequest, errorResponse{"environment too long (max 32)"})
		return
	}
	p, err := h.store.CreateProject(name, env)
	if err != nil {
		if isUniqueErr(err) {
			writeJSON(w, http.StatusConflict, errorResponse{"project (name, environment) already exists"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to create project"})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// UpdateProject changes name + environment. Admin only.
func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProjectID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	var req struct {
		Name        string `json:"name"`
		Environment string `json:"environment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}
	name := strings.TrimSpace(req.Name)
	env := strings.TrimSpace(req.Environment)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"name required"})
		return
	}
	if len(name) > maxProjectName {
		writeJSON(w, http.StatusBadRequest, errorResponse{"name too long (max 64)"})
		return
	}
	if len(env) > maxProjectEnv {
		writeJSON(w, http.StatusBadRequest, errorResponse{"environment too long (max 32)"})
		return
	}
	if err := h.store.UpdateProject(id, name, env); err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, errorResponse{"project not found"})
			return
		}
		if isUniqueErr(err) {
			writeJSON(w, http.StatusConflict, errorResponse{"project (name, environment) already exists"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to update project"})
		return
	}
	p, _ := h.store.GetProject(id)
	writeJSON(w, http.StatusOK, p)
}

// DeleteProject removes a project. Returns 409 if any agents still belong to it.
func (h *Handler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProjectID(w, r)
	if !ok {
		return
	}
	if err := h.store.DeleteProject(id); err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, errorResponse{"project not found"})
			return
		}
		if errors.Is(err, store.ErrProjectHasAgents) {
			writeJSON(w, http.StatusConflict, errorResponse{"project has agents — move or delete them first"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to delete project"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ProjectAgents returns all agents in a given project, scoped by developer
// permissions when applicable.
func (h *Handler) ProjectAgents(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProjectID(w, r)
	if !ok {
		return
	}
	allowed, err := h.userAllowedAgents(r)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to resolve permissions"})
		return
	}
	agents, err := h.store.ListAgentsByProject(id, allowed)
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

// MoveAgent reassigns an agent to a different project. Admin only.
func (h *Handler) MoveAgent(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	if agentID == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"agent id required"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	var req struct {
		ProjectID int64 `json:"project_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}
	if req.ProjectID <= 0 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"project_id required"})
		return
	}
	if _, err := h.store.GetProject(req.ProjectID); err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusBadRequest, errorResponse{"project not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to verify project"})
		return
	}
	if err := h.store.MoveAgentToProject(agentID, req.ProjectID); err != nil {
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, errorResponse{"agent not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to move agent"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func parseProjectID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid project id"})
		return 0, false
	}
	return id, true
}
