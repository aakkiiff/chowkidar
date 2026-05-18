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
	s, err := New(":memory:", 0)
	if err != nil {
		t.Fatalf("New(:memory:): %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// projectID creates a fresh test project and returns its id. Each call uses
// the test name + a counter so multiple projects in one test don't collide.
var projectCounter int64

func projectID(t *testing.T, s *Store) int64 {
	t.Helper()
	projectCounter++
	p, err := s.CreateProject(fmt.Sprintf("test-%s-%d", t.Name(), projectCounter), "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return p.ID
}

func TestHasUsers_Empty(t *testing.T) {
	s := newTestStore(t)
	has, err := s.HasUsers()
	if err != nil {
		t.Fatalf("HasUsers: %v", err)
	}
	if has {
		t.Error("expected false on empty store")
	}
}

func TestHasUsers_WithUser(t *testing.T) {
	s := newTestStore(t)
	hash, _ := bcrypt.GenerateFromPassword([]byte("pass"), bcrypt.MinCost)
	if _, err := s.CreateAppUser("admin", string(hash), RoleAdmin); err != nil {
		t.Fatalf("CreateAppUser: %v", err)
	}
	has, err := s.HasUsers()
	if err != nil {
		t.Fatalf("HasUsers: %v", err)
	}
	if !has {
		t.Error("expected true after creating a user")
	}
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
	id, err := s.CreateAgent("myhost", hashTok(token), projectID(t, s))
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
	id, err := s.CreateAgent("reporthost", hashTok("tok1"), projectID(t, s))
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
	aid1, _ := s.CreateAgent("agent1", hashTok("t1"), projectID(t, s))
	aid2, _ := s.CreateAgent("agent2", hashTok("t2"), projectID(t, s))

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
	agentID, _ := s.CreateAgent("ephost", hashTok("eptok"), projectID(t, s))
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
	agentID, _ := s.CreateAgent("probehost", hashTok("probetok"), projectID(t, s))
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
	agentID, _ := s.CreateAgent("inchost", hashTok("inctok"), projectID(t, s))
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
	agentID, _ := s.CreateAgent("uptimehost", hashTok("uptimetok"), projectID(t, s))
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
	id, _ := s.CreateAgent("oldname", hashTok("rt1"), projectID(t, s))
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
	id, _ := s.CreateAgent("delhost", hashTok("dt1"), projectID(t, s))
	if err := s.DeleteAgent(id); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}
	_, err := s.GetAgentWithMetrics(id)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows after delete, got %v", err)
	}
}

// ── Projects ──────────────────────────────────────────────────────────────────

func TestProjects_CreateGet(t *testing.T) {
	s := newTestStore(t)
	p, err := s.CreateProject("backend", "prod")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if p.Name != "backend" || p.Environment != "prod" {
		t.Errorf("got %+v", p)
	}
	got, err := s.GetProject(p.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if got.ID != p.ID {
		t.Errorf("expected id %d, got %d", p.ID, got.ID)
	}
}

func TestProjects_DuplicateNameEnv_Conflict(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("dup", "prod"); err != nil {
		t.Fatalf("first: %v", err)
	}
	_, err := s.CreateProject("dup", "prod")
	if err == nil {
		t.Fatal("expected UNIQUE error")
	}
}

func TestProjects_SameName_DifferentEnv_OK(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateProject("myapp", "prod"); err != nil {
		t.Fatalf("prod: %v", err)
	}
	if _, err := s.CreateProject("myapp", "staging"); err != nil {
		t.Errorf("staging should be allowed: %v", err)
	}
}

func TestProjects_List_AgentCount(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("p1", "")
	if _, err := s.CreateAgent("h1", hashTok("x1"), p.ID); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if _, err := s.CreateAgent("h2", hashTok("x2"), p.ID); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	list, err := s.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 project, got %d", len(list))
	}
	if list[0].AgentCount != 2 {
		t.Errorf("expected agent_count=2, got %d", list[0].AgentCount)
	}
}

func TestProjects_Update(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("old-name", "dev")
	if err := s.UpdateProject(p.ID, "new-name", "prod"); err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	got, _ := s.GetProject(p.ID)
	if got.Name != "new-name" || got.Environment != "prod" {
		t.Errorf("got %+v", got)
	}
}

func TestProjects_Delete_BlockedWithAgents(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("with-agents", "")
	s.CreateAgent("h", hashTok("t"), p.ID)
	err := s.DeleteProject(p.ID)
	if err != ErrProjectHasAgents {
		t.Errorf("expected ErrProjectHasAgents, got %v", err)
	}
}

func TestProjects_Delete_Empty(t *testing.T) {
	s := newTestStore(t)
	p, _ := s.CreateProject("empty", "")
	if err := s.DeleteProject(p.ID); err != nil {
		t.Errorf("expected nil err, got %v", err)
	}
}

func TestProjects_BackfillDefault_OnExistingAgents(t *testing.T) {
	s := newTestStore(t)
	// Insert agent directly with NULL project_id to simulate pre-migration row.
	if _, err := s.db.Exec(`INSERT INTO agents (id, hostname, token_hash) VALUES ('legacy-id', 'legacy-host', 'h')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.backfillDefaultProject(); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	// Default project should exist and the legacy agent should be assigned to it.
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM agents WHERE project_id IS NULL`).Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 agents with NULL project_id, got %d", count)
	}
	var defID int64
	if err := s.db.QueryRow(`SELECT id FROM projects WHERE name = 'default'`).Scan(&defID); err != nil {
		t.Fatalf("default project not created: %v", err)
	}
}

func TestListAgentsByProject_FiltersToProject(t *testing.T) {
	s := newTestStore(t)
	p1, _ := s.CreateProject("p1", "")
	p2, _ := s.CreateProject("p2", "")
	s.CreateAgent("h1", hashTok("k1"), p1.ID)
	s.CreateAgent("h2", hashTok("k2"), p2.ID)

	list, err := s.ListAgentsByProject(p1.ID, nil)
	if err != nil {
		t.Fatalf("ListAgentsByProject: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
	if list[0].Hostname != "h1" {
		t.Errorf("expected h1, got %s", list[0].Hostname)
	}
}

// ── Alert events ──────────────────────────────────────────────────────────────

func TestSaveAlertEvent_AndRecent(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()
	_, err := s.SaveAlertEvent(AlertEvent{
		AgentID: "agt1", Hostname: "host1", Metric: "cpu", Phase: "fired",
		Value: 92, Threshold: 85, SustainedFor: "30s", FiredAt: now,
	})
	if err != nil {
		t.Fatalf("SaveAlertEvent: %v", err)
	}
	_, err = s.SaveAlertEvent(AlertEvent{
		AgentID: "agt1", Hostname: "host1", Metric: "cpu", Phase: "resolved",
		Value: 60, Threshold: 85, FiredAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("SaveAlertEvent recovery: %v", err)
	}

	events, err := s.RecentAlertEvents(10)
	if err != nil {
		t.Fatalf("RecentAlertEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	// Newest first → resolved first.
	if events[0].Phase != "resolved" {
		t.Errorf("expected resolved at top, got %s", events[0].Phase)
	}
	if events[1].Phase != "fired" {
		t.Errorf("expected fired at bottom, got %s", events[1].Phase)
	}
}

func TestUnseenAlertCount_AndMarkSeen(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 3; i++ {
		_, err := s.SaveAlertEvent(AlertEvent{
			AgentID: "agt1", Hostname: "h", Metric: "cpu", Phase: "fired",
			Value: 90, Threshold: 80, FiredAt: time.Now(),
		})
		if err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	n, err := s.UnseenAlertCount()
	if err != nil {
		t.Fatalf("UnseenAlertCount: %v", err)
	}
	if n != 3 {
		t.Errorf("expected 3 unseen, got %d", n)
	}

	marked, err := s.MarkAllAlertsSeen()
	if err != nil {
		t.Fatalf("MarkAllAlertsSeen: %v", err)
	}
	if marked != 3 {
		t.Errorf("expected 3 marked, got %d", marked)
	}

	n, _ = s.UnseenAlertCount()
	if n != 0 {
		t.Errorf("expected 0 unseen after mark, got %d", n)
	}

	// Mark-seen on no unseen rows = 0 affected
	marked, _ = s.MarkAllAlertsSeen()
	if marked != 0 {
		t.Errorf("expected 0 on repeat mark, got %d", marked)
	}
}

func TestAlertRetention_GetSet_Bounds(t *testing.T) {
	s := newTestStore(t)
	if got := s.GetAlertRetentionDays(); got != 7 {
		t.Errorf("expected default 7, got %d", got)
	}
	if err := s.SetAlertRetentionDays(14); err != nil {
		t.Errorf("SetAlertRetentionDays 14: %v", err)
	}
	if got := s.GetAlertRetentionDays(); got != 14 {
		t.Errorf("expected 14, got %d", got)
	}
	if err := s.SetAlertRetentionDays(0); err == nil {
		t.Error("expected error for 0 days")
	}
	if err := s.SetAlertRetentionDays(91); err == nil {
		t.Error("expected error for 91 days")
	}
}
