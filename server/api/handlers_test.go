package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/technonext/chowkidar/server/alert"
	"github.com/technonext/chowkidar/server/logbroker"
	"github.com/technonext/chowkidar/server/logstore"
	"github.com/technonext/chowkidar/server/store"
	"golang.org/x/crypto/bcrypt"
)

const (
	handlerSecret = "handlers-test-secret-minimum-32chars!!"
	adminUser     = "admin"
	adminPass     = "adminpass1234" // ≥12 chars to pass handler validation
)

func setupHandler(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	s, err := store.New(":memory:", 0)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	hashed, _ := bcrypt.GenerateFromPassword([]byte(adminPass), bcrypt.MinCost)
	if _, err := s.CreateAppUser(adminUser, string(hashed), RoleAdmin); err != nil {
		t.Fatalf("CreateAppUser: %v", err)
	}

	ls, err := logstore.New(logstore.Config{
		Dir:           t.TempDir(),
		MaxFileBytes:  1024 * 1024,
		MaxRotations:  5,
		RetentionDays: 14,
	})
	if err != nil {
		t.Fatalf("logstore.New: %v", err)
	}
	br := logbroker.New()
	ab := alert.NewBroker()
	h := NewHandler(s, ls, br, ab, handlerSecret, false, 200)
	return h.Routes(), s
}

// setupHandlerEmpty returns a handler with no users — for setup endpoint tests.
func setupHandlerEmpty(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	s, err := store.New(":memory:", 0)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	ls, err := logstore.New(logstore.Config{
		Dir:           t.TempDir(),
		MaxFileBytes:  1024 * 1024,
		MaxRotations:  5,
		RetentionDays: 14,
	})
	if err != nil {
		t.Fatalf("logstore.New: %v", err)
	}
	br := logbroker.New()
	ab := alert.NewBroker()
	h := NewHandler(s, ls, br, ab, handlerSecret, false, 200)
	return h.Routes(), s
}

// adminCookie logs in as admin and returns the session cookie value (name=value).
func adminCookie(t *testing.T, mux http.Handler) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": adminUser, "password": adminPass})
	req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}
	setCookie := w.Header().Get("Set-Cookie")
	if setCookie == "" {
		t.Fatal("expected Set-Cookie header from login")
	}
	return strings.SplitN(setCookie, ";", 2)[0]
}

// developerCookie creates a developer user and logs in as them.
func developerCookie(t *testing.T, mux http.Handler, s *store.Store) string {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("devpassword1234"), bcrypt.MinCost)
	if _, err := s.CreateAppUser("devuser", string(hash), RoleDeveloper); err != nil {
		t.Fatalf("CreateAppUser developer: %v", err)
	}
	body, _ := json.Marshal(map[string]string{"username": "devuser", "password": "devpassword1234"})
	req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("developer login failed: %d %s", w.Code, w.Body.String())
	}
	setCookie := w.Header().Get("Set-Cookie")
	return strings.SplitN(setCookie, ";", 2)[0]
}

// doJSON sends a JSON request with an optional session cookie.
func doJSON(t *testing.T, mux http.Handler, method, path, cookie string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	} else {
		buf = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// doJSONBearer sends a JSON request with a Bearer token (for agent endpoints).
func doJSONBearer(t *testing.T, mux http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	} else {
		buf = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// registerAgent registers an agent and returns its ID and bearer token.
func registerAgent(t *testing.T, mux http.Handler, cookie, hostname string) (id, token string) {
	t.Helper()
	w := doJSON(t, mux, "POST", "/api/v1/agents/register", cookie, map[string]string{"hostname": hostname})
	if w.Code != http.StatusOK {
		t.Fatalf("register agent failed: %d %s", w.Code, w.Body.String())
	}
	var reg map[string]string
	json.NewDecoder(w.Body).Decode(&reg)
	return reg["agent_id"], reg["token"]
}

// ── Health ────────────────────────────────────────────────────────────────────

func TestHealth(t *testing.T) {
	mux, _ := setupHandler(t)
	w := doJSON(t, mux, "GET", "/api/v1/health", "", nil)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Errorf(`expected {"status":"ok"}, got %v`, resp)
	}
}

// ── Setup ─────────────────────────────────────────────────────────────────────

func TestSetupStatus_NeedsSetup(t *testing.T) {
	mux, _ := setupHandlerEmpty(t)
	w := doJSON(t, mux, "GET", "/api/v1/setup/status", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]bool
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp["setup_needed"] {
		t.Error("expected setup_needed=true on empty store")
	}
}

func TestSetupStatus_AlreadyDone(t *testing.T) {
	mux, _ := setupHandler(t)
	w := doJSON(t, mux, "GET", "/api/v1/setup/status", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]bool
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["setup_needed"] {
		t.Error("expected setup_needed=false when users exist")
	}
}

func TestSetup_CreatesAdminUser(t *testing.T) {
	mux, s := setupHandlerEmpty(t)
	w := doJSON(t, mux, "POST", "/api/v1/setup", "", map[string]string{
		"password": "strongpassword123",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["username"] != "admin" {
		t.Errorf("expected username admin, got %s", resp["username"])
	}
	has, _ := s.HasUsers()
	if !has {
		t.Error("expected user to exist in store after setup")
	}
}

func TestSetup_AlreadyDone_Returns403(t *testing.T) {
	mux, _ := setupHandler(t)
	w := doJSON(t, mux, "POST", "/api/v1/setup", "", map[string]string{
		"password": "strongpassword123",
	})
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestSetup_PasswordTooShort(t *testing.T) {
	mux, _ := setupHandlerEmpty(t)
	w := doJSON(t, mux, "POST", "/api/v1/setup", "", map[string]string{
		"password": "short",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ── Auth ──────────────────────────────────────────────────────────────────────

func TestLogin_Success(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)
	if cookie == "" {
		t.Fatal("expected non-empty session cookie")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	mux, _ := setupHandler(t)
	w := doJSON(t, mux, "POST", "/api/v1/auth/login", "", map[string]string{
		"username": adminUser, "password": "wrong",
	})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestLogin_UnknownUser(t *testing.T) {
	mux, _ := setupHandler(t)
	w := doJSON(t, mux, "POST", "/api/v1/auth/login", "", map[string]string{
		"username": "nobody", "password": "irrelevant",
	})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestLogin_EmptyBody(t *testing.T) {
	mux, _ := setupHandler(t)
	w := doJSON(t, mux, "POST", "/api/v1/auth/login", "", map[string]string{
		"username": "", "password": "",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestLogout_ClearsCookie(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)

	w := doJSON(t, mux, "POST", "/api/v1/auth/logout", cookie, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	setCookie := w.Header().Get("Set-Cookie")
	if !strings.Contains(setCookie, "Max-Age=0") && !strings.Contains(setCookie, "Max-Age=-1") {
		t.Errorf("expected logout to clear cookie (Max-Age≤0), got: %s", setCookie)
	}
}

func TestMe_NoToken(t *testing.T) {
	mux, _ := setupHandler(t)
	w := doJSON(t, mux, "GET", "/api/v1/auth/me", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestMe_ValidCookie(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)
	w := doJSON(t, mux, "GET", "/api/v1/auth/me", cookie, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["username"] != adminUser {
		t.Errorf("expected username %s, got %s", adminUser, resp["username"])
	}
	if resp["role"] != RoleAdmin {
		t.Errorf("expected admin role, got %s", resp["role"])
	}
}

func TestChangeOwnPassword(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)

	w := doJSON(t, mux, "POST", "/api/v1/auth/password", cookie, map[string]string{
		"current_password": adminPass,
		"new_password":     "newadminpass5678",
	})
	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Old password should no longer work.
	w2 := doJSON(t, mux, "POST", "/api/v1/auth/login", "", map[string]string{
		"username": adminUser, "password": adminPass,
	})
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("old password still accepted after change")
	}

	// New password should work.
	w3 := doJSON(t, mux, "POST", "/api/v1/auth/login", "", map[string]string{
		"username": adminUser, "password": "newadminpass5678",
	})
	if w3.Code != http.StatusOK {
		t.Errorf("new password rejected: %d %s", w3.Code, w3.Body.String())
	}
}

func TestChangeOwnPassword_WrongCurrent(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)
	w := doJSON(t, mux, "POST", "/api/v1/auth/password", cookie, map[string]string{
		"current_password": "notright123456",
		"new_password":     "newpassword5678",
	})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong current password, got %d", w.Code)
	}
}

// ── Role enforcement ──────────────────────────────────────────────────────────

func TestDeveloperRole_ForbiddenOnAdminRoute(t *testing.T) {
	mux, s := setupHandler(t)
	devCookie := developerCookie(t, mux, s)

	// Admin-only routes should return 403 for developer.
	adminRoutes := []struct{ method, path string }{
		{"POST", "/api/v1/agents/register"},
		{"GET", "/api/v1/users"},
		{"POST", "/api/v1/users"},
		{"GET", "/api/v1/webhooks"},
	}
	for _, r := range adminRoutes {
		w := doJSON(t, mux, r.method, r.path, devCookie, map[string]string{})
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s: expected 403 for developer, got %d", r.method, r.path, w.Code)
		}
	}
}

func TestDeveloperRole_CanReadAgents(t *testing.T) {
	mux, s := setupHandler(t)
	devCookie := developerCookie(t, mux, s)

	w := doJSON(t, mux, "GET", "/api/v1/agents", devCookie, nil)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for developer reading agents, got %d", w.Code)
	}
}

// ── Agents ────────────────────────────────────────────────────────────────────

func TestListAgents_EmptyArray(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)
	w := doJSON(t, mux, "GET", "/api/v1/agents", cookie, nil)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := strings.TrimSpace(w.Body.String())
	if !strings.HasPrefix(body, "[") {
		t.Errorf("expected JSON array, got: %s", body)
	}
}

func TestRegisterAgent_And_GetAgent(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)
	agentID, agentToken := registerAgent(t, mux, cookie, "test-host")
	if agentToken == "" {
		t.Fatal("expected non-empty agent token")
	}

	w := doJSON(t, mux, "GET", fmt.Sprintf("/api/v1/agents/%s", agentID), cookie, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GetAgent failed: %d %s", w.Code, w.Body.String())
	}
	var agent map[string]any
	json.NewDecoder(w.Body).Decode(&agent)
	if agent["id"] != agentID {
		t.Errorf("expected agent id %s, got %v", agentID, agent["id"])
	}
}

func TestRenameAgent(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)
	agentID, _ := registerAgent(t, mux, cookie, "old-host")

	w := doJSON(t, mux, "PATCH", fmt.Sprintf("/api/v1/agents/%s", agentID), cookie, map[string]string{"hostname": "new-host"})
	if w.Code != http.StatusOK {
		t.Fatalf("RenameAgent failed: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["hostname"] != "new-host" {
		t.Errorf("expected new-host, got %s", resp["hostname"])
	}
}

func TestDeleteAgent_ThenGet404(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)
	agentID, _ := registerAgent(t, mux, cookie, "del-host")

	w := doJSON(t, mux, "DELETE", fmt.Sprintf("/api/v1/agents/%s", agentID), cookie, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("Delete failed: %d", w.Code)
	}
	w2 := doJSON(t, mux, "GET", fmt.Sprintf("/api/v1/agents/%s", agentID), cookie, nil)
	if w2.Code != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", w2.Code)
	}
}

// ── Report ────────────────────────────────────────────────────────────────────

func TestReport_ValidToken(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)
	_, agentToken := registerAgent(t, mux, cookie, "report-host")

	report := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"system": map[string]any{
			"cpu_percent": 25.5, "mem_total_gb": 16.0, "mem_used_gb": 4.0,
			"disk_total_gb": 500.0, "disk_used_gb": 100.0,
		},
		"containers": []any{},
	}
	w := doJSONBearer(t, mux, "POST", "/api/v1/report", agentToken, report)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestReport_InvalidToken(t *testing.T) {
	mux, _ := setupHandler(t)
	report := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"system":    map[string]any{},
		"containers": []any{},
	}
	w := doJSONBearer(t, mux, "POST", "/api/v1/report", "agt_invalid000", report)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestReport_NoToken(t *testing.T) {
	mux, _ := setupHandler(t)
	w := doJSONBearer(t, mux, "POST", "/api/v1/report", "", map[string]any{})
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAgentContainers_AfterReport(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)
	agentID, agentToken := registerAgent(t, mux, cookie, "ctr-host")

	report := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"system": map[string]any{
			"cpu_percent": 10.0, "mem_total_gb": 8.0, "mem_used_gb": 2.0,
			"disk_total_gb": 100.0, "disk_used_gb": 20.0,
		},
		"containers": []any{
			map[string]any{
				"id": "abc123", "name": "nginx", "image": "nginx:latest",
				"status": "running", "cpu_percent": 2.5, "mem_used_mb": 64.0, "mem_limit_mb": 256.0,
			},
		},
	}
	doJSONBearer(t, mux, "POST", "/api/v1/report", agentToken, report)

	w := doJSON(t, mux, "GET", fmt.Sprintf("/api/v1/agents/%s/containers", agentID), cookie, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("AgentContainers: %d %s", w.Code, w.Body.String())
	}
	var ctrs []any
	json.NewDecoder(w.Body).Decode(&ctrs)
	if len(ctrs) != 1 {
		t.Errorf("expected 1 container, got %d", len(ctrs))
	}
}

// ── Users ─────────────────────────────────────────────────────────────────────

func TestListUsers(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)
	w := doJSON(t, mux, "GET", "/api/v1/users", cookie, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("ListUsers: %d %s", w.Code, w.Body.String())
	}
	var users []any
	json.NewDecoder(w.Body).Decode(&users)
	if len(users) == 0 {
		t.Error("expected at least one user (admin)")
	}
}

func TestCreateUser_Developer(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)
	w := doJSON(t, mux, "POST", "/api/v1/users", cookie, map[string]any{
		"username": "devuser",
		"password": "devpassword5678",
		"role":     "developer",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("CreateUser: %d %s", w.Code, w.Body.String())
	}
	var u map[string]any
	json.NewDecoder(w.Body).Decode(&u)
	if u["username"] != "devuser" {
		t.Errorf("expected devuser, got %v", u["username"])
	}
}

func TestCreateUser_PasswordTooShort(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)
	w := doJSON(t, mux, "POST", "/api/v1/users", cookie, map[string]any{
		"username": "shortpass",
		"password": "tooshort",
		"role":     "developer",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for short password, got %d", w.Code)
	}
}

func TestCreateUser_PasswordTooLong(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)
	w := doJSON(t, mux, "POST", "/api/v1/users", cookie, map[string]any{
		"username": "longpass",
		"password": strings.Repeat("a", 73),
		"role":     "developer",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for password >72 chars, got %d", w.Code)
	}
}

func TestCreateUser_DuplicateUsername(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)
	body := map[string]any{"username": "dupuser", "password": "duppassword123", "role": "developer"}
	doJSON(t, mux, "POST", "/api/v1/users", cookie, body)
	w := doJSON(t, mux, "POST", "/api/v1/users", cookie, body)
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 for duplicate username, got %d", w.Code)
	}
}

func TestDeleteUser(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)

	w := doJSON(t, mux, "POST", "/api/v1/users", cookie, map[string]any{
		"username": "todelete",
		"password": "deletepassword12",
		"role":     "developer",
	})
	var u map[string]any
	json.NewDecoder(w.Body).Decode(&u)
	userID := int(u["id"].(float64))

	w2 := doJSON(t, mux, "DELETE", fmt.Sprintf("/api/v1/users/%d", userID), cookie, nil)
	if w2.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestSetUserPassword_ByAdmin(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)

	w := doJSON(t, mux, "POST", "/api/v1/users", cookie, map[string]any{
		"username": "pwuser",
		"password": "initial-password12",
		"role":     "developer",
	})
	var u map[string]any
	json.NewDecoder(w.Body).Decode(&u)
	userID := int(u["id"].(float64))

	w2 := doJSON(t, mux, "PUT", fmt.Sprintf("/api/v1/users/%d/password", userID), cookie, map[string]string{
		"password": "newpassword5678x",
	})
	if w2.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w2.Code, w2.Body.String())
	}
}

// ── Endpoints ─────────────────────────────────────────────────────────────────

func TestEndpoint_CRUD(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)
	agentID, _ := registerAgent(t, mux, cookie, "ep-host")

	// Create
	wc := doJSON(t, mux, "POST", fmt.Sprintf("/api/v1/agents/%s/endpoints", agentID), cookie, map[string]any{
		"name": "homepage", "url": "https://example.com",
	})
	if wc.Code != http.StatusOK {
		t.Fatalf("CreateEndpoint: %d %s", wc.Code, wc.Body.String())
	}
	var ep map[string]any
	json.NewDecoder(wc.Body).Decode(&ep)
	epID := int(ep["id"].(float64))

	// List
	wl := doJSON(t, mux, "GET", fmt.Sprintf("/api/v1/agents/%s/endpoints", agentID), cookie, nil)
	if wl.Code != http.StatusOK {
		t.Fatalf("ListEndpoints: %d", wl.Code)
	}
	var eps []any
	json.NewDecoder(wl.Body).Decode(&eps)
	if len(eps) != 1 {
		t.Errorf("expected 1 endpoint, got %d", len(eps))
	}

	// Delete
	wd := doJSON(t, mux, "DELETE", fmt.Sprintf("/api/v1/endpoints/%d", epID), cookie, nil)
	if wd.Code != http.StatusNoContent {
		t.Errorf("DeleteEndpoint: expected 204, got %d", wd.Code)
	}

	wl2 := doJSON(t, mux, "GET", fmt.Sprintf("/api/v1/agents/%s/endpoints", agentID), cookie, nil)
	var eps2 []any
	json.NewDecoder(wl2.Body).Decode(&eps2)
	if len(eps2) != 0 {
		t.Errorf("expected 0 endpoints after delete, got %d", len(eps2))
	}
}

// ── Webhooks ──────────────────────────────────────────────────────────────────

func TestWebhook_CRUD(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)

	// Create
	wc := doJSON(t, mux, "POST", "/api/v1/webhooks", cookie, map[string]any{
		"name": "discord-alert", "url": "https://discord.com/api/webhooks/123/abc", "type": "discord",
	})
	if wc.Code != http.StatusOK {
		t.Fatalf("CreateWebhook: %d %s", wc.Code, wc.Body.String())
	}
	var wh map[string]any
	json.NewDecoder(wc.Body).Decode(&wh)
	whID := int(wh["id"].(float64))

	// List
	wl := doJSON(t, mux, "GET", "/api/v1/webhooks", cookie, nil)
	if wl.Code != http.StatusOK {
		t.Fatalf("ListWebhooks: %d", wl.Code)
	}
	var hooks []any
	json.NewDecoder(wl.Body).Decode(&hooks)
	if len(hooks) != 1 {
		t.Errorf("expected 1 webhook, got %d", len(hooks))
	}

	// Delete
	wd := doJSON(t, mux, "DELETE", fmt.Sprintf("/api/v1/webhooks/%d", whID), cookie, nil)
	if wd.Code != http.StatusNoContent {
		t.Errorf("DeleteWebhook: expected 204, got %d", wd.Code)
	}
}
