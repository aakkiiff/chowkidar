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
	handlerSecret   = "handlers-test-secret"
	adminUser       = "admin"
	adminPass       = "adminpass"
)

func setupHandler(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	s, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	hashed, _ := bcrypt.GenerateFromPassword([]byte(adminPass), bcrypt.MinCost)
	if err := s.CreateUser(adminUser, string(hashed)); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	ls, err := logstore.New(logstore.Config{
		Dir:          t.TempDir(),
		MaxFileBytes: 1024 * 1024,
		MaxRotations: 5,
		RetentionDays: 14,
	})
	if err != nil {
		t.Fatalf("logstore.New: %v", err)
	}
	br := logbroker.New()
	ab := alert.NewBroker()
	h := NewHandler(s, ls, br, ab, handlerSecret)
	return h.Routes(), s
}

func adminToken(t *testing.T, mux http.Handler) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": adminUser, "password": adminPass})
	req := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	return resp["token"]
}

func doJSON(t *testing.T, mux http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
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

func TestLogin_Success(t *testing.T) {
	mux, _ := setupHandler(t)
	tok := adminToken(t, mux)
	if tok == "" {
		t.Fatal("expected non-empty token")
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

func TestMe_NoToken(t *testing.T) {
	mux, _ := setupHandler(t)
	w := doJSON(t, mux, "GET", "/api/v1/auth/me", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestMe_ValidToken(t *testing.T) {
	mux, _ := setupHandler(t)
	tok := adminToken(t, mux)
	w := doJSON(t, mux, "GET", "/api/v1/auth/me", tok, nil)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
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

func TestListAgents_EmptyArray(t *testing.T) {
	mux, _ := setupHandler(t)
	tok := adminToken(t, mux)
	w := doJSON(t, mux, "GET", "/api/v1/agents", tok, nil)
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
	tok := adminToken(t, mux)

	// Register
	w := doJSON(t, mux, "POST", "/api/v1/agents/register", tok, map[string]string{"hostname": "test-host"})
	if w.Code != http.StatusOK {
		t.Fatalf("register failed: %d %s", w.Code, w.Body.String())
	}
	var reg map[string]string
	json.NewDecoder(w.Body).Decode(&reg)
	agentID := reg["agent_id"]
	if agentID == "" {
		t.Fatal("expected non-empty agent_id")
	}
	if reg["token"] == "" {
		t.Fatal("expected non-empty token")
	}

	// Get
	w2 := doJSON(t, mux, "GET", fmt.Sprintf("/api/v1/agents/%s", agentID), tok, nil)
	if w2.Code != http.StatusOK {
		t.Fatalf("GetAgent failed: %d %s", w2.Code, w2.Body.String())
	}
	var agent map[string]any
	json.NewDecoder(w2.Body).Decode(&agent)
	if agent["id"] != agentID {
		t.Errorf("expected agent id %s, got %v", agentID, agent["id"])
	}
}

func TestRenameAgent(t *testing.T) {
	mux, _ := setupHandler(t)
	tok := adminToken(t, mux)

	w := doJSON(t, mux, "POST", "/api/v1/agents/register", tok, map[string]string{"hostname": "old-host"})
	var reg map[string]string
	json.NewDecoder(w.Body).Decode(&reg)
	agentID := reg["agent_id"]

	w2 := doJSON(t, mux, "PATCH", fmt.Sprintf("/api/v1/agents/%s", agentID), tok, map[string]string{"hostname": "new-host"})
	if w2.Code != http.StatusOK {
		t.Fatalf("RenameAgent failed: %d %s", w2.Code, w2.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w2.Body).Decode(&resp)
	if resp["hostname"] != "new-host" {
		t.Errorf("expected new-host, got %s", resp["hostname"])
	}
}

func TestDeleteAgent_ThenGet404(t *testing.T) {
	mux, _ := setupHandler(t)
	tok := adminToken(t, mux)

	w := doJSON(t, mux, "POST", "/api/v1/agents/register", tok, map[string]string{"hostname": "del-host"})
	var reg map[string]string
	json.NewDecoder(w.Body).Decode(&reg)
	agentID := reg["agent_id"]

	w2 := doJSON(t, mux, "DELETE", fmt.Sprintf("/api/v1/agents/%s", agentID), tok, nil)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("Delete failed: %d", w2.Code)
	}
	w3 := doJSON(t, mux, "GET", fmt.Sprintf("/api/v1/agents/%s", agentID), tok, nil)
	if w3.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w3.Code)
	}
}

func TestReport_ValidToken(t *testing.T) {
	mux, _ := setupHandler(t)
	tok := adminToken(t, mux)

	w := doJSON(t, mux, "POST", "/api/v1/agents/register", tok, map[string]string{"hostname": "report-host"})
	var reg map[string]string
	json.NewDecoder(w.Body).Decode(&reg)
	agentToken := reg["token"]

	report := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"system": map[string]any{
			"cpu_percent":   25.5,
			"mem_total_gb":  16.0,
			"mem_used_gb":   4.0,
			"disk_total_gb": 500.0,
			"disk_used_gb":  100.0,
		},
		"containers": []any{},
	}
	req := httptest.NewRequest("POST", "/api/v1/report", nil)
	b, _ := json.Marshal(report)
	req = httptest.NewRequest("POST", "/api/v1/report", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+agentToken)
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req)
	if w2.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestReport_InvalidToken(t *testing.T) {
	mux, _ := setupHandler(t)
	report := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"system":    map[string]any{},
		"containers": []any{},
	}
	b, _ := json.Marshal(report)
	req := httptest.NewRequest("POST", "/api/v1/report", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer agt_invalid000")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestListUsers(t *testing.T) {
	mux, _ := setupHandler(t)
	tok := adminToken(t, mux)
	w := doJSON(t, mux, "GET", "/api/v1/users", tok, nil)
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
	tok := adminToken(t, mux)
	w := doJSON(t, mux, "POST", "/api/v1/users", tok, map[string]any{
		"username": "devuser",
		"password": "devpass123",
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

func TestDeleteUser(t *testing.T) {
	mux, _ := setupHandler(t)
	tok := adminToken(t, mux)

	w := doJSON(t, mux, "POST", "/api/v1/users", tok, map[string]any{
		"username": "todelete",
		"password": "pass123",
		"role":     "developer",
	})
	var u map[string]any
	if err := json.NewDecoder(w.Body).Decode(&u); err != nil {
		t.Fatalf("decode user: %v", err)
	}
	idVal, ok := u["id"]
	if !ok {
		t.Fatalf("no id in response: %v", u)
	}
	userID := int(idVal.(float64))

	w2 := doJSON(t, mux, "DELETE", fmt.Sprintf("/api/v1/users/%d", userID), tok, nil)
	if w2.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestAgentContainers_AfterReport(t *testing.T) {
	mux, _ := setupHandler(t)
	tok := adminToken(t, mux)

	w := doJSON(t, mux, "POST", "/api/v1/agents/register", tok, map[string]string{"hostname": "ctr-host"})
	var reg map[string]string
	json.NewDecoder(w.Body).Decode(&reg)
	agentID := reg["agent_id"]
	agentToken := reg["token"]

	// Send a report with one container
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
	b, _ := json.Marshal(report)
	req := httptest.NewRequest("POST", "/api/v1/report", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+agentToken)
	httptest.NewRecorder() // discard
	mux.ServeHTTP(httptest.NewRecorder(), req)

	w2 := doJSON(t, mux, "GET", fmt.Sprintf("/api/v1/agents/%s/containers", agentID), tok, nil)
	if w2.Code != http.StatusOK {
		t.Fatalf("AgentContainers: %d %s", w2.Code, w2.Body.String())
	}
	var ctrs []any
	json.NewDecoder(w2.Body).Decode(&ctrs)
	if len(ctrs) != 1 {
		t.Errorf("expected 1 container, got %d", len(ctrs))
	}
}
