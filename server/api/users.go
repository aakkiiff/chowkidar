package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/technonext/chowkidar/server/store"
	"golang.org/x/crypto/bcrypt"
)

// validRole guards inputs from arbitrary strings to one of the known roles.
func validRole(r string) bool {
	return r == RoleAdmin || r == RoleDeveloper
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.store.ListUsers()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to list users"})
		return
	}
	if users == nil {
		users = []store.AppUser{}
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string   `json:"username"`
		Password string   `json:"password"`
		Role     string   `json:"role"`
		AgentIDs []string `json:"agent_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" || len(req.Password) < 12 || len(req.Password) > 72 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"username and password (12–72 chars) required"})
		return
	}
	if len(username) > 64 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"username too long"})
		return
	}
	if !validRole(req.Role) {
		writeJSON(w, http.StatusBadRequest, errorResponse{"role must be admin or developer"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"hash failed"})
		return
	}
	u, err := h.store.CreateAppUser(username, string(hash), req.Role)
	if err != nil {
		if isUniqueErr(err) {
			writeJSON(w, http.StatusConflict, errorResponse{"username already in use"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to create user"})
		return
	}
	if req.Role == "developer" && len(req.AgentIDs) > 0 {
		if err := h.store.SetUserAgentPerms(u.ID, req.AgentIDs); err != nil {
			writeJSON(w, http.StatusInternalServerError, errorResponse{"user created but failed to set agent permissions"})
			return
		}
		u.AgentIDs = req.AgentIDs
	}
	writeJSON(w, http.StatusOK, u)
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid id"})
		return
	}
	// Prevent deleting your own account so an admin can't accidentally lock
	// themselves out before another admin exists.
	caller, _ := r.Context().Value(ctxKeyUsername).(string)
	users, _ := h.store.ListUsers()
	for _, u := range users {
		if u.ID == id && u.Username == caller {
			writeJSON(w, http.StatusBadRequest, errorResponse{"cannot delete the currently signed-in user"})
			return
		}
	}
	if err := h.store.DeleteAppUser(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, errorResponse{"user not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to delete user"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SetUserPassword resets a user's password. Admin only.
func (h *Handler) SetUserPassword(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid id"})
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}
	if len(req.Password) < 12 || len(req.Password) > 72 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"password must be 12–72 chars"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"hash failed"})
		return
	}
	if err := h.store.UpdateUserPassword(id, string(hash)); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, errorResponse{"user not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to set password"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ChangeOwnPassword lets any authenticated user change their own password.
// Requires the current password for verification.
func (h *Handler) ChangeOwnPassword(w http.ResponseWriter, r *http.Request) {
	username, _ := r.Context().Value(ctxKeyUsername).(string)
	var req struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}
	if len(req.New) < 12 || len(req.New) > 72 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"new password must be 12–72 chars"})
		return
	}
	if req.Current == req.New {
		writeJSON(w, http.StatusBadRequest, errorResponse{"new password must differ from current"})
		return
	}
	id, hashedPassword, _, err := h.store.GetUser(username)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to load user"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(req.Current)); err != nil {
		writeJSON(w, http.StatusUnauthorized, errorResponse{"current password is incorrect"})
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(req.New), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"hash failed"})
		return
	}
	if err := h.store.UpdateUserPassword(int64(id), string(newHash)); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to update password"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SetUserAgents replaces the set of agents a developer can see.
func (h *Handler) SetUserAgents(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid id"})
		return
	}
	var req struct {
		AgentIDs []string `json:"agent_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}
	if err := h.store.SetUserAgentPerms(id, req.AgentIDs); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to save permissions"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
