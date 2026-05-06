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

// ListProjects returns all projects with their agent counts.
func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.store.ListProjects()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to list projects"})
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

// GetProject returns one project by id.
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

func parseProjectID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid project id"})
		return 0, false
	}
	return id, true
}
