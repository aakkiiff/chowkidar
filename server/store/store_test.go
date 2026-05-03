package store

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func hashTok(token string) string {
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", h)
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(":memory:")
	if err != nil {
		t.Fatalf("New(:memory:): %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNew_InMemory(t *testing.T) {
	s := newTestStore(t)
	if s == nil {
		t.Fatal("expected non-nil store")
	}
}

func TestCreateAgent_ValidateToken(t *testing.T) {
	s := newTestStore(t)
	token := "agt_testtoken123"
	id, err := s.CreateAgent("myhost", hashTok(token))
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty agent id")
	}
	got, err := s.ValidateToken(hashTok(token))
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if got != id {
		t.Errorf("expected id %s, got %s", id, got)
	}
}

func TestListAgentsWithMetrics_InitiallyEmpty(t *testing.T) {
	s := newTestStore(t)
	agents, err := s.ListAgentsWithMetrics(nil)
	if err != nil {
		t.Fatalf("ListAgentsWithMetrics: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}
}

func TestSaveReport_GetLatestContainers(t *testing.T) {
	s := newTestStore(t)
	id, err := s.CreateAgent("reporthost", hashTok("tok1"))
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	sys := SystemMetrics{CPUPercent: 42.5, MemUsedGB: 2, MemTotalGB: 8}
	containers := []ContainerMetrics{
		{ID: "cid1", Name: "web", Image: "nginx", Status: "running", CPUPercent: 5, MemUsedMB: 100, MemLimitMB: 512},
	}
	if err := s.SaveReport(id, time.Now(), sys, containers); err != nil {
		t.Fatalf("SaveReport: %v", err)
	}
	ctrs, err := s.GetLatestContainers(id)
	if err != nil {
		t.Fatalf("GetLatestContainers: %v", err)
	}
	if len(ctrs) != 1 {
		t.Fatalf("expected 1 container, got %d", len(ctrs))
	}
	if ctrs[0].Name != "web" {
		t.Errorf("expected container name web, got %s", ctrs[0].Name)
	}
}

func TestCreateAppUser_GetUser(t *testing.T) {
	s := newTestStore(t)
	hash, _ := bcrypt.GenerateFromPassword([]byte("pass123"), bcrypt.MinCost)
	u, err := s.CreateAppUser("devuser", string(hash), RoleDeveloper)
	if err != nil {
		t.Fatalf("CreateAppUser: %v", err)
	}
	if u.Username != "devuser" {
		t.Errorf("expected devuser, got %s", u.Username)
	}
	_, pw, role, err := s.GetUser("devuser")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if role != RoleDeveloper {
		t.Errorf("expected developer role, got %s", role)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(pw), []byte("pass123")); err != nil {
		t.Errorf("password mismatch: %v", err)
	}
}

func TestCreateAppUser_DuplicateReturnsError(t *testing.T) {
	s := newTestStore(t)
	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.MinCost)
	if _, err := s.CreateAppUser("dupuser", string(hash), RoleAdmin); err != nil {
		t.Fatalf("first CreateAppUser: %v", err)
	}
	_, err := s.CreateAppUser("dupuser", string(hash), RoleAdmin)
	if err == nil {
		t.Fatal("expected conflict error for duplicate username")
	}
	if !strings.Contains(err.Error(), "UNIQUE") {
		t.Errorf("expected UNIQUE error, got: %v", err)
	}
}

func TestDeleteAppUser(t *testing.T) {
	s := newTestStore(t)
	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.MinCost)
	u, err := s.CreateAppUser("todelete", string(hash), RoleAdmin)
	if err != nil {
		t.Fatalf("CreateAppUser: %v", err)
	}
	if err := s.DeleteAppUser(u.ID); err != nil {
		t.Fatalf("DeleteAppUser: %v", err)
	}
	_, _, _, err = s.GetUser("todelete")
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows after delete, got %v", err)
	}
}

func TestUpdateUserPassword(t *testing.T) {
	s := newTestStore(t)
	hash, _ := bcrypt.GenerateFromPassword([]byte("old"), bcrypt.MinCost)
	u, _ := s.CreateAppUser("pwuser", string(hash), RoleAdmin)
	newHash, _ := bcrypt.GenerateFromPassword([]byte("new"), bcrypt.MinCost)
	if err := s.UpdateUserPassword(u.ID, string(newHash)); err != nil {
		t.Fatalf("UpdateUserPassword: %v", err)
	}
	_, pw, _, _ := s.GetUser("pwuser")
	if err := bcrypt.CompareHashAndPassword([]byte(pw), []byte("new")); err != nil {
		t.Errorf("updated password doesn't match: %v", err)
	}
}

const RoleDeveloper = "developer"
const RoleAdmin = "admin"

func TestSetUserAgentPerms_GetUserAgentPerms(t *testing.T) {
	s := newTestStore(t)
	hash, _ := bcrypt.GenerateFromPassword([]byte("p"), bcrypt.MinCost)
	u, _ := s.CreateAppUser("devperm", string(hash), RoleDeveloper)
	aid1, _ := s.CreateAgent("agent1", hashTok("t1"))
	aid2, _ := s.CreateAgent("agent2", hashTok("t2"))

	if err := s.SetUserAgentPerms(u.ID, []string{aid1, aid2}); err != nil {
		t.Fatalf("SetUserAgentPerms: %v", err)
	}
	perms, err := s.GetUserAgentPerms(u.ID)
	if err != nil {
		t.Fatalf("GetUserAgentPerms: %v", err)
	}
	if len(perms) != 2 {
		t.Errorf("expected 2 perms, got %d", len(perms))
	}
}

func TestCreateWebhook_ListWebhooks_DeleteWebhook(t *testing.T) {
	s := newTestStore(t)
	w, err := s.CreateWebhook("my-hook", "https://discord.com/webhook/1", "discord")
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if w.Name != "my-hook" {
		t.Errorf("expected my-hook, got %s", w.Name)
	}
	hooks, err := s.ListWebhooks()
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if len(hooks) != 1 {
		t.Errorf("expected 1 webhook, got %d", len(hooks))
	}
	if err := s.DeleteWebhook(w.ID); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	hooks, _ = s.ListWebhooks()
	if len(hooks) != 0 {
		t.Errorf("expected 0 webhooks after delete, got %d", len(hooks))
	}
}

func TestSetAlertSettings_GetAlertSettings(t *testing.T) {
	s := newTestStore(t)
	in := AlertSettings{SustainSeconds: 120, ResendCooldownSeconds: 300}
	if err := s.SetAlertSettings(in); err != nil {
		t.Fatalf("SetAlertSettings: %v", err)
	}
	out := s.GetAlertSettings()
	if out.SustainSeconds != 120 {
		t.Errorf("expected 120, got %d", out.SustainSeconds)
	}
	if out.ResendCooldownSeconds != 300 {
		t.Errorf("expected 300, got %d", out.ResendCooldownSeconds)
	}
}

func TestCreateEndpoint_ListEndpoints_DeleteEndpoint(t *testing.T) {
	s := newTestStore(t)
	agentID, _ := s.CreateAgent("ephost", hashTok("eptok"))
	ep, err := s.CreateEndpoint(agentID, "homepage", "https://example.com")
	if err != nil {
		t.Fatalf("CreateEndpoint: %v", err)
	}
	if ep.Name != "homepage" {
		t.Errorf("expected homepage, got %s", ep.Name)
	}
	eps, err := s.ListEndpoints(agentID)
	if err != nil {
		t.Fatalf("ListEndpoints: %v", err)
	}
	if len(eps) != 1 {
		t.Errorf("expected 1 endpoint, got %d", len(eps))
	}
	if err := s.DeleteEndpoint(ep.ID); err != nil {
		t.Fatalf("DeleteEndpoint: %v", err)
	}
	eps, _ = s.ListEndpoints(agentID)
	if len(eps) != 0 {
		t.Errorf("expected 0 endpoints after delete, got %d", len(eps))
	}
}

func TestRecordProbe_GetEndpointProbes(t *testing.T) {
	s := newTestStore(t)
	agentID, _ := s.CreateAgent("probehost", hashTok("probetok"))
	ep, _ := s.CreateEndpoint(agentID, "health", "https://example.com/health")
	now := time.Now().UTC().Truncate(time.Second)
	probe := EndpointProbe{
		EndpointID: ep.ID,
		ProbedAt:   now,
		StatusCode: 200,
		LatencyMS:  45,
		OK:         true,
	}
	if err := s.RecordProbe(probe); err != nil {
		t.Fatalf("RecordProbe: %v", err)
	}
	probes, err := s.GetEndpointProbes(ep.ID, now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("GetEndpointProbes: %v", err)
	}
	if len(probes) != 1 {
		t.Fatalf("expected 1 probe, got %d", len(probes))
	}
	if probes[0].StatusCode != 200 {
		t.Errorf("expected 200, got %d", probes[0].StatusCode)
	}
}

func TestOpenIncident_CloseIncident_ListIncidents(t *testing.T) {
	s := newTestStore(t)
	agentID, _ := s.CreateAgent("inchost", hashTok("inctok"))
	ep, _ := s.CreateEndpoint(agentID, "api", "https://example.com/api")
	start := time.Now().Add(-5 * time.Minute).UTC()

	incID, err := s.OpenIncident(ep.ID, start, 503, "service unavailable")
	if err != nil {
		t.Fatalf("OpenIncident: %v", err)
	}
	if incID == 0 {
		t.Fatal("expected non-zero incident id")
	}
	closeTime := time.Now().UTC()
	if err := s.CloseIncident(ep.ID, closeTime); err != nil {
		t.Fatalf("CloseIncident: %v", err)
	}
	incs, err := s.ListIncidents(ep.ID, start.Add(-time.Minute))
	if err != nil {
		t.Fatalf("ListIncidents: %v", err)
	}
	if len(incs) != 1 {
		t.Fatalf("expected 1 incident, got %d", len(incs))
	}
	if incs[0].EndedAt == nil {
		t.Error("expected incident to be closed")
	}
}

func TestComputeUptime_NoIncidents_Returns100(t *testing.T) {
	s := newTestStore(t)
	agentID, _ := s.CreateAgent("uptimehost", hashTok("uptimetok"))
	ep, _ := s.CreateEndpoint(agentID, "check", "https://example.com")
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()
	stats, err := s.ComputeUptime(ep.ID, start, end)
	if err != nil {
		t.Fatalf("ComputeUptime: %v", err)
	}
	if stats.Percent != 100 {
		t.Errorf("expected 100%% uptime, got %.2f", stats.Percent)
	}
	if stats.IncidentCount != 0 {
		t.Errorf("expected 0 incidents, got %d", stats.IncidentCount)
	}
}

func TestRenameAgent(t *testing.T) {
	s := newTestStore(t)
	id, _ := s.CreateAgent("oldname", hashTok("rt1"))
	if err := s.RenameAgent(id, "newname"); err != nil {
		t.Fatalf("RenameAgent: %v", err)
	}
	a, err := s.GetAgentWithMetrics(id)
	if err != nil {
		t.Fatalf("GetAgentWithMetrics: %v", err)
	}
	if a.Hostname != "newname" {
		t.Errorf("expected newname, got %s", a.Hostname)
	}
}

func TestDeleteAgent(t *testing.T) {
	s := newTestStore(t)
	id, _ := s.CreateAgent("delhost", hashTok("dt1"))
	if err := s.DeleteAgent(id); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}
	_, err := s.GetAgentWithMetrics(id)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows after delete, got %v", err)
	}
}
