package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// ── Projects ──────────────────────────────────────────────────────────────────

func TestProjects_CreateAndList(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)

	wc := doJSON(t, mux, "POST", "/api/v1/projects", cookie, map[string]any{
		"name":        "backend-services",
		"environment": "prod",
	})
	if wc.Code != http.StatusOK {
		t.Fatalf("CreateProject: %d %s", wc.Code, wc.Body.String())
	}
	var p map[string]any
	json.NewDecoder(wc.Body).Decode(&p)
	if p["name"] != "backend-services" {
		t.Errorf("expected name backend-services, got %v", p["name"])
	}
	if p["environment"] != "prod" {
		t.Errorf("expected env prod, got %v", p["environment"])
	}

	wl := doJSON(t, mux, "GET", "/api/v1/projects", cookie, nil)
	if wl.Code != http.StatusOK {
		t.Fatalf("ListProjects: %d", wl.Code)
	}
	var list []any
	json.NewDecoder(wl.Body).Decode(&list)
	if len(list) != 1 {
		t.Errorf("expected 1 project, got %d", len(list))
	}
}

func TestProjects_DuplicateNameEnv_Returns409(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)

	body := map[string]any{"name": "dup", "environment": "prod"}
	doJSON(t, mux, "POST", "/api/v1/projects", cookie, body)
	w := doJSON(t, mux, "POST", "/api/v1/projects", cookie, body)
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 for duplicate, got %d", w.Code)
	}
}

func TestProjects_SameName_DifferentEnv_OK(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)

	doJSON(t, mux, "POST", "/api/v1/projects", cookie, map[string]any{"name": "myapp", "environment": "prod"})
	w := doJSON(t, mux, "POST", "/api/v1/projects", cookie, map[string]any{"name": "myapp", "environment": "staging"})
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for same name different env, got %d: %s", w.Code, w.Body.String())
	}
}

func TestProjects_EmptyName_Returns400(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)

	w := doJSON(t, mux, "POST", "/api/v1/projects", cookie, map[string]any{"name": "  ", "environment": ""})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty name, got %d", w.Code)
	}
}

func TestProjects_NameTooLong_Returns400(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)

	w := doJSON(t, mux, "POST", "/api/v1/projects", cookie, map[string]any{
		"name":        strings.Repeat("a", 65),
		"environment": "",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for name too long, got %d", w.Code)
	}
}

func TestProjects_Update(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)

	pid := createProject(t, mux, cookie)
	w := doJSON(t, mux, "PATCH", fmt.Sprintf("/api/v1/projects/%d", pid), cookie, map[string]any{
		"name":        "renamed",
		"environment": "staging",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateProject: %d %s", w.Code, w.Body.String())
	}
	var p map[string]any
	json.NewDecoder(w.Body).Decode(&p)
	if p["name"] != "renamed" {
		t.Errorf("expected renamed, got %v", p["name"])
	}
	if p["environment"] != "staging" {
		t.Errorf("expected staging, got %v", p["environment"])
	}
}

func TestProjects_Update_NonExistent_Returns404(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)

	w := doJSON(t, mux, "PATCH", "/api/v1/projects/99999", cookie, map[string]any{
		"name": "anything", "environment": "",
	})
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestProjects_Delete_Empty(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)

	pid := createProject(t, mux, cookie)
	w := doJSON(t, mux, "DELETE", fmt.Sprintf("/api/v1/projects/%d", pid), cookie, nil)
	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}
}

func TestProjects_Delete_WithAgents_Returns409(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)

	pid := createProject(t, mux, cookie)
	wr := doJSON(t, mux, "POST", "/api/v1/agents/register", cookie, map[string]any{
		"hostname":   "agent-in-project",
		"project_id": pid,
	})
	if wr.Code != http.StatusOK {
		t.Fatalf("register agent: %d %s", wr.Code, wr.Body.String())
	}

	w := doJSON(t, mux, "DELETE", fmt.Sprintf("/api/v1/projects/%d", pid), cookie, nil)
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 when project has agents, got %d", w.Code)
	}
}

func TestProjects_GetByID(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)

	pid := createProject(t, mux, cookie)
	w := doJSON(t, mux, "GET", fmt.Sprintf("/api/v1/projects/%d", pid), cookie, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GetProject: %d %s", w.Code, w.Body.String())
	}
	var p map[string]any
	json.NewDecoder(w.Body).Decode(&p)
	if int64(p["id"].(float64)) != pid {
		t.Errorf("expected id %d, got %v", pid, p["id"])
	}
}

func TestProjects_GetByID_NotFound(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)

	w := doJSON(t, mux, "GET", "/api/v1/projects/99999", cookie, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestProjects_AgentsByProject(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)

	// Create two projects, register one agent in each.
	pid1 := createProject(t, mux, cookie)
	pid2 := createProject(t, mux, cookie)

	doJSON(t, mux, "POST", "/api/v1/agents/register", cookie, map[string]any{
		"hostname": "host-p1", "project_id": pid1,
	})
	doJSON(t, mux, "POST", "/api/v1/agents/register", cookie, map[string]any{
		"hostname": "host-p2", "project_id": pid2,
	})

	w := doJSON(t, mux, "GET", fmt.Sprintf("/api/v1/projects/%d/agents", pid1), cookie, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("ProjectAgents: %d %s", w.Code, w.Body.String())
	}
	var agents []map[string]any
	json.NewDecoder(w.Body).Decode(&agents)
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent in project, got %d", len(agents))
	}
	if agents[0]["hostname"] != "host-p1" {
		t.Errorf("expected host-p1, got %v", agents[0]["hostname"])
	}
}

func TestProjects_DeveloperForbiddenOnWrite(t *testing.T) {
	mux, s := setupHandler(t)
	devCookie := developerCookie(t, mux, s)

	w := doJSON(t, mux, "POST", "/api/v1/projects", devCookie, map[string]any{
		"name": "dev-attempt", "environment": "",
	})
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for developer creating project, got %d", w.Code)
	}
}

func TestProjects_DeveloperCanRead(t *testing.T) {
	mux, s := setupHandler(t)
	devCookie := developerCookie(t, mux, s)

	w := doJSON(t, mux, "GET", "/api/v1/projects", devCookie, nil)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for developer listing projects, got %d", w.Code)
	}
}

func TestProjects_DeveloperOnlySeesPermittedProjects(t *testing.T) {
	mux, s := setupHandler(t)
	cookie := adminCookie(t, mux)
	devCookie := developerCookie(t, mux, s)

	// Two projects, two agents (one in each).
	pidA := createProject(t, mux, cookie)
	pidB := createProject(t, mux, cookie)

	wA := doJSON(t, mux, "POST", "/api/v1/agents/register", cookie, map[string]any{
		"hostname": "host-A", "project_id": pidA,
	})
	var regA map[string]string
	json.NewDecoder(wA.Body).Decode(&regA)

	doJSON(t, mux, "POST", "/api/v1/agents/register", cookie, map[string]any{
		"hostname": "host-B", "project_id": pidB,
	})

	// Grant the developer access only to host-A (in project A).
	var devID int64
	s.GetUser("devuser") // ensures devuser exists
	users, _ := s.ListUsers()
	for _, u := range users {
		if u.Username == "devuser" {
			devID = u.ID
		}
	}
	if err := s.SetUserAgentPerms(devID, []string{regA["agent_id"]}); err != nil {
		t.Fatalf("SetUserAgentPerms: %v", err)
	}

	// Developer's project list should contain only project A.
	wl := doJSON(t, mux, "GET", "/api/v1/projects", devCookie, nil)
	if wl.Code != http.StatusOK {
		t.Fatalf("ListProjects: %d %s", wl.Code, wl.Body.String())
	}
	var list []map[string]any
	json.NewDecoder(wl.Body).Decode(&list)
	if len(list) != 1 {
		t.Fatalf("expected 1 visible project, got %d", len(list))
	}
	if int64(list[0]["id"].(float64)) != pidA {
		t.Errorf("expected project A visible, got id %v", list[0]["id"])
	}

	// Developer should be able to GET project A.
	wgA := doJSON(t, mux, "GET", fmt.Sprintf("/api/v1/projects/%d", pidA), devCookie, nil)
	if wgA.Code != http.StatusOK {
		t.Errorf("expected 200 for permitted project, got %d", wgA.Code)
	}

	// Developer should get 404 on project B (existence hidden).
	wgB := doJSON(t, mux, "GET", fmt.Sprintf("/api/v1/projects/%d", pidB), devCookie, nil)
	if wgB.Code != http.StatusNotFound {
		t.Errorf("expected 404 for forbidden project, got %d", wgB.Code)
	}
}

func TestRegisterAgent_MissingProjectID_Returns400(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)

	w := doJSON(t, mux, "POST", "/api/v1/agents/register", cookie, map[string]any{
		"hostname": "no-project",
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing project_id, got %d", w.Code)
	}
}

func TestRegisterAgent_InvalidProjectID_Returns400(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)

	w := doJSON(t, mux, "POST", "/api/v1/agents/register", cookie, map[string]any{
		"hostname":   "ghost-host",
		"project_id": 99999,
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid project_id, got %d", w.Code)
	}
}

func TestMoveAgent_BetweenProjects(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)

	pid1 := createProject(t, mux, cookie)
	pid2 := createProject(t, mux, cookie)

	wr := doJSON(t, mux, "POST", "/api/v1/agents/register", cookie, map[string]any{
		"hostname": "movable-agent", "project_id": pid1,
	})
	var reg map[string]string
	json.NewDecoder(wr.Body).Decode(&reg)
	agentID := reg["agent_id"]

	w := doJSON(t, mux, "PUT", fmt.Sprintf("/api/v1/agents/%s/project", agentID), cookie, map[string]any{
		"project_id": pid2,
	})
	if w.Code != http.StatusNoContent {
		t.Fatalf("MoveAgent: expected 204, got %d %s", w.Code, w.Body.String())
	}

	// Verify agent now in pid2.
	wg := doJSON(t, mux, "GET", "/api/v1/agents/"+agentID, cookie, nil)
	var a map[string]any
	json.NewDecoder(wg.Body).Decode(&a)
	if int64(a["project_id"].(float64)) != pid2 {
		t.Errorf("expected agent in project %d, got %v", pid2, a["project_id"])
	}

	// And not in pid1's listing.
	wl := doJSON(t, mux, "GET", fmt.Sprintf("/api/v1/projects/%d/agents", pid1), cookie, nil)
	var list []any
	json.NewDecoder(wl.Body).Decode(&list)
	if len(list) != 0 {
		t.Errorf("expected 0 agents in source project, got %d", len(list))
	}
}

func TestMoveAgent_InvalidProject_Returns400(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)
	agentID, _ := registerAgent(t, mux, cookie, "stuck-host")

	w := doJSON(t, mux, "PUT", "/api/v1/agents/"+agentID+"/project", cookie, map[string]any{
		"project_id": 99999,
	})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestMoveAgent_DeveloperForbidden(t *testing.T) {
	mux, s := setupHandler(t)
	devCookie := developerCookie(t, mux, s)
	w := doJSON(t, mux, "PUT", "/api/v1/agents/anything/project", devCookie, map[string]any{
		"project_id": 1,
	})
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestAgentResponse_IncludesProjectInfo(t *testing.T) {
	mux, _ := setupHandler(t)
	cookie := adminCookie(t, mux)

	pid := createProject(t, mux, cookie)
	wr := doJSON(t, mux, "POST", "/api/v1/agents/register", cookie, map[string]any{
		"hostname":   "host-with-project",
		"project_id": pid,
	})
	var reg map[string]string
	json.NewDecoder(wr.Body).Decode(&reg)

	wg := doJSON(t, mux, "GET", "/api/v1/agents/"+reg["agent_id"], cookie, nil)
	if wg.Code != http.StatusOK {
		t.Fatalf("GetAgent: %d %s", wg.Code, wg.Body.String())
	}
	var a map[string]any
	json.NewDecoder(wg.Body).Decode(&a)
	if int64(a["project_id"].(float64)) != pid {
		t.Errorf("expected project_id %d, got %v", pid, a["project_id"])
	}
}
