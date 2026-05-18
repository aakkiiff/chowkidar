// Defaults to same-origin path — works with Vite dev proxy and nginx reverse
// proxy. Override with VITE_API_BASE when hosting frontend and server on
// different origins.
const API_BASE = (import.meta.env.VITE_API_BASE ?? '/api/v1').replace(/\/$/, '');

// Auth is handled exclusively via httpOnly cookie set by the server on login.
// credentials: 'include' ensures the browser sends the cookie on every request.
// Authorization headers are NOT sent — the token is never accessible to JS.
// Paths whose 401 means "bad credentials" — NOT "session expired". For these
// we let the server's error message bubble through unmodified and skip the
// clearSession() side effect (which fires an unwanted logout call).
const AUTH_PATHS = ['/auth/login', '/setup'];

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    credentials: 'include',
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    ...options,
  });

  if (res.status === 401 && !AUTH_PATHS.some(p => path.startsWith(p))) {
    clearSession();
    throw new Error('Session expired');
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || 'Request failed');
  }
  return res.json();
}

// ── Setup ─────────────────────────────────────────────────────────────────────

export async function getSetupStatus(): Promise<{ setup_needed: boolean }> {
  const res = await fetch(`${API_BASE}/setup/status`, { credentials: 'include' });
  if (!res.ok) return { setup_needed: false };
  return res.json();
}

export async function setupAdmin(password: string): Promise<void> {
  const res = await fetch(`${API_BASE}/setup`, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || 'Setup failed');
  }
}

// ── Auth ──────────────────────────────────────────────────────────────────────

export type Role = 'admin' | 'developer';

// login posts credentials and receives the session cookie from the server.
// Returns username+role for UI display; the JWT itself stays in the httpOnly cookie.
export function login(username: string, password: string) {
  return request<{ username: string; role: Role }>('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  });
}

export function me(_token: string) {
  return request<{ username: string; role: Role }>('/auth/me');
}

// ── User management (admin only) ──────────────────────────────────────────────

export interface AppUser {
  id: number;
  username: string;
  role: Role;
  agent_ids?: string[];
  created_at: string;
}

export function listUsers(_token: string) {
  return request<AppUser[]>('/users');
}

export function createUser(_token: string, username: string, password: string, role: Role, agentIds?: string[]) {
  return request<AppUser>('/users', {
    method: 'POST',
    body: JSON.stringify({ username, password, role, agent_ids: agentIds }),
  });
}

export async function deleteUser(_token: string, id: number): Promise<void> {
  const res = await fetch(`${API_BASE}/users/${id}`, {
    method: 'DELETE',
    credentials: 'include',
  });
  if (res.status === 401) { clearSession(); throw new Error('Session expired'); }
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || 'Delete failed');
  }
}

export async function setUserPassword(_token: string, id: number, password: string): Promise<void> {
  const res = await fetch(`${API_BASE}/users/${id}/password`, {
    method: 'PUT',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
  });
  if (res.status === 401) { clearSession(); throw new Error('Session expired'); }
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || 'Update failed');
  }
}

export async function changeOwnPassword(_token: string, currentPassword: string, newPassword: string): Promise<void> {
  const res = await fetch(`${API_BASE}/auth/password`, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
  });
  if (res.status === 401) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || 'Unauthorized');
  }
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || 'Update failed');
  }
}

export async function setUserAgents(_token: string, userId: number, agentIds: string[]): Promise<void> {
  const res = await fetch(`${API_BASE}/users/${userId}/agents`, {
    method: 'PUT',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ agent_ids: agentIds }),
  });
  if (res.status === 401) { clearSession(); throw new Error('Session expired'); }
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || 'Update failed');
  }
}

// ── Projects ──────────────────────────────────────────────────────────────────

export interface Project {
  id: number;
  name: string;
  environment: string;
  created_at: string;
  agent_count: number;
}

export function listProjects() {
  return request<Project[]>('/projects');
}

export function getProject(id: number) {
  return request<Project>(`/projects/${id}`);
}

export function listProjectAgents(id: number) {
  return request<Agent[]>(`/projects/${id}/agents`);
}

export function createProject(name: string, environment: string) {
  return request<Project>('/projects', {
    method: 'POST',
    body: JSON.stringify({ name, environment }),
  });
}

export function updateProject(id: number, name: string, environment: string) {
  return request<Project>(`/projects/${id}`, {
    method: 'PATCH',
    body: JSON.stringify({ name, environment }),
  });
}

export async function deleteProject(id: number): Promise<void> {
  const res = await fetch(`${API_BASE}/projects/${id}`, {
    method: 'DELETE',
    credentials: 'include',
  });
  if (res.status === 401) { clearSession(); throw new Error('Session expired'); }
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || 'Delete failed');
  }
}

// ── Agents ────────────────────────────────────────────────────────────────────

export interface Agent {
  id: string;
  hostname: string;
  last_seen: string | null;
  cpu_percent: number | null;
  mem_used_gb: number | null;
  mem_total_gb: number | null;
  disk_used_gb: number | null;
  disk_total_gb: number | null;
  container_count: number;
  alerts_enabled: boolean;
  active_issues: number;
  project_id: number;
  project_name: string;
  project_environment: string;
}

export function listAgents(_token: string) {
  return request<Agent[]>('/agents');
}

export function getAgent(_token: string, agentId: string) {
  return request<Agent>(`/agents/${agentId}`);
}

export async function moveAgentToProject(agentId: string, projectId: number): Promise<void> {
  const res = await fetch(`${API_BASE}/agents/${agentId}/project`, {
    method: 'PUT',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ project_id: projectId }),
  });
  if (res.status === 401) { clearSession(); throw new Error('Session expired'); }
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || 'Move failed');
  }
}

export function registerAgent(_token: string, hostname: string, projectId: number) {
  return request<{ agent_id: string; token: string }>('/agents/register', {
    method: 'POST',
    body: JSON.stringify({ hostname, project_id: projectId }),
  });
}

export interface AlertRule {
  agent_id: string;
  cpu_enabled: boolean;
  cpu_threshold: number;
  mem_enabled: boolean;
  mem_threshold: number;
  disk_enabled: boolean;
  disk_threshold: number;
  ctr_down_enabled: boolean;
  ctr_cpu_enabled: boolean;
  ctr_cpu_threshold_mcore: number;
  ctr_mem_enabled: boolean;
  ctr_mem_threshold: number;
  endpoint_down_enabled: boolean;
  ssl_alert_enabled: boolean;
  agent_down_enabled: boolean;
  webhook_id: number | null;
}

export function getAlertRule(_token: string, agentId: string) {
  return request<AlertRule>(`/agents/${agentId}/alert-rule`);
}

export interface AlertSettings {
  sustain_seconds: number;
  resend_cooldown_seconds: number;
}

export function getAlertSettings(_token: string) {
  return request<AlertSettings>('/settings/alerts');
}

export function saveAlertSettings(_token: string, s: AlertSettings) {
  return request<AlertSettings>('/settings/alerts', {
    method: 'PUT',
    body: JSON.stringify(s),
  });
}

export function saveAlertRule(_token: string, rule: AlertRule) {
  return request<AlertRule>(`/agents/${rule.agent_id}/alert-rule`, {
    method: 'PUT',
    body: JSON.stringify(rule),
  });
}

export function setAgentAlerts(_token: string, agentId: string, enabled: boolean) {
  return request<{ id: string; alerts_enabled: boolean }>(`/agents/${agentId}/alerts`, {
    method: 'PATCH',
    body: JSON.stringify({ enabled }),
  });
}

export function renameAgent(_token: string, agentId: string, hostname: string) {
  return request<{ id: string; hostname: string }>(`/agents/${agentId}`, {
    method: 'PATCH',
    body: JSON.stringify({ hostname }),
  });
}

export async function deleteAgent(_token: string, agentId: string): Promise<void> {
  const res = await fetch(`${API_BASE}/agents/${agentId}`, {
    method: 'DELETE',
    credentials: 'include',
  });
  if (res.status === 401) { clearSession(); throw new Error('Session expired'); }
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || 'Delete failed');
  }
}

// ── Agent detail ──────────────────────────────────────────────────────────────

export interface Container {
  id: string;
  name: string;
  image: string;
  status: string;
  cpu_percent: number;
  mem_used_mb: number;
  mem_limit_mb: number;
  restart_count: number;
  started_at: string;
  net_rx_mb: number;
  net_tx_mb: number;
}

export function getAgentContainers(_token: string, agentId: string) {
  return request<Container[]>(`/agents/${agentId}/containers`);
}

export interface ContainerPoint {
  timestamp: string;
  cpu_percent: number;
  mem_used_mb: number;
  mem_limit_mb: number;
}

export type HistoryRange = number;

export function getContainerHistory(
  _token: string,
  agentId: string,
  name: string,
  minutes: HistoryRange,
) {
  return request<{ points: ContainerPoint[] }>(
    `/agents/${agentId}/containers/${encodeURIComponent(name)}/history?range=${minutes}`,
  );
}

// ── Alert event stream (SSE) ──────────────────────────────────────────────────

export type AlertPhase = 'observed' | 'fired' | 'resolved';
export type AlertMetric =
  | 'cpu' | 'memory' | 'disk'
  | 'container_cpu' | 'container_memory' | 'container_down'
  | 'endpoint_down' | 'ssl_expiring' | 'agent_down';

export interface AlertEvent {
  agent_id: string;
  hostname: string;
  metric: AlertMetric;
  container_name?: string;
  endpoint_name?: string;
  endpoint_url?: string;
  value: number;
  threshold: number;
  sustained_for: string;
  timestamp: string;
  phase: AlertPhase;
  /** @deprecated kept for compatibility with older server builds */
  resolved: boolean;
}

// PersistedAlertEvent extends AlertEvent with the DB row's id + read state.
// Returned by /alerts/recent and the SSE `backlog` event.
export interface PersistedAlertEvent extends AlertEvent {
  id: number;
  fired_at: string;
  seen_at: string | null;
}

export function streamAlerts(
  _token: string,
  onAlert: (e: AlertEvent) => void,
  onBacklog: (events: PersistedAlertEvent[]) => void,
  onError: (err: unknown) => void,
): AbortController {
  const ctrl = new AbortController();

  const connect = async (): Promise<'retry' | 'stop'> => {
    try {
      const res = await fetch(`${API_BASE}/alerts/stream`, {
        credentials: 'include',
        signal: ctrl.signal,
      });
      if (res.status === 401) {
        clearSession();
        onError(new Error('Session expired'));
        return 'stop';
      }
      if (!res.ok || !res.body) throw new Error(`stream failed: ${res.status}`);

      const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
      let buf = '';
      while (true) {
        const { done, value } = await reader.read();
        if (done) return 'retry';
        buf += value;
        let idx: number;
        while ((idx = buf.indexOf('\n\n')) >= 0) {
          const frame = buf.slice(0, idx);
          buf = buf.slice(idx + 2);
          if (frame.startsWith(':')) continue;
          // SSE frames are `event: <name>\ndata: <json>`. Default event name
          // is `message`. Server emits `backlog` once on connect + `alert` per
          // live event. Match the event line to route into the right handler.
          const eventLine = frame.split('\n').find(l => l.startsWith('event: '));
          const dataLine = frame.split('\n').find(l => l.startsWith('data: '));
          if (!dataLine) continue;
          const eventName = eventLine?.slice(7).trim() ?? 'alert';
          try {
            const payload = JSON.parse(dataLine.slice(6));
            if (eventName === 'backlog') onBacklog(payload as PersistedAlertEvent[]);
            else onAlert(payload as AlertEvent);
          } catch { /* ignore */ }
        }
      }
    } catch (err) {
      if ((err as Error).name === 'AbortError') return 'stop';
      onError(err);
      return 'retry';
    }
  };

  (async () => {
    let backoff = 1000;
    while (!ctrl.signal.aborted) {
      const outcome = await connect();
      if (outcome === 'stop') return;
      await new Promise(r => setTimeout(r, backoff));
      backoff = Math.min(backoff * 2, 30_000);
    }
  })();

  return ctrl;
}

// ── Log fetch + streaming ─────────────────────────────────────────────────────

export function getRecentLogs(
  _token: string,
  agentId: string,
  name: string,
  minutes: number,
) {
  return request<LogLine[]>(
    `/agents/${agentId}/containers/${encodeURIComponent(name)}/logs?minutes=${minutes}`,
  );
}

export interface LogLine {
  container_id: string;
  container_name: string;
  stream: 'stdout' | 'stderr';
  timestamp: string;
  text: string;
}

export function streamLogs(
  _token: string,
  agentId: string,
  name: string,
  tail: number,
  onLine: (l: LogLine) => void,
  onError: (err: unknown) => void,
): AbortController {
  const ctrl = new AbortController();
  const url = `${API_BASE}/agents/${agentId}/containers/${encodeURIComponent(name)}/logs/tail?tail=${tail}`;

  (async () => {
    try {
      const res = await fetch(url, {
        credentials: 'include',
        signal: ctrl.signal,
      });
      if (res.status === 401) { clearSession(); throw new Error('Session expired'); }
      if (!res.ok || !res.body) throw new Error(`stream failed: ${res.status}`);

      const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
      let buf = '';
      while (true) {
        const { done, value } = await reader.read();
        if (done) return;
        buf += value;
        let idx: number;
        while ((idx = buf.indexOf('\n\n')) >= 0) {
          const frame = buf.slice(0, idx);
          buf = buf.slice(idx + 2);
          if (frame.startsWith(':')) continue;
          const dataLine = frame.split('\n').find(l => l.startsWith('data: '));
          if (!dataLine) continue;
          try { onLine(JSON.parse(dataLine.slice(6)) as LogLine); } catch { /* ignore */ }
        }
      }
    } catch (err) {
      if ((err as Error).name === 'AbortError') return;
      onError(err);
    }
  })();

  return ctrl;
}

// ── Endpoint monitoring ───────────────────────────────────────────────────────

export interface Endpoint {
  id: number;
  agent_id: string;
  name: string;
  url: string;
  alert_on_down: boolean;
  created_at: string;
  last_probed_at: string | null;
  last_status_code: number | null;
  last_latency_ms: number | null;
  last_ok: boolean | null;
  last_error?: string;
  last_cert_not_after?: string | null;
}

export interface EndpointProbe {
  id: number;
  endpoint_id: number;
  probed_at: string;
  status_code: number;
  latency_ms: number;
  ok: boolean;
  error?: string;
  cert_not_after?: string | null;
}

export interface EndpointIncident {
  id: number;
  endpoint_id: number;
  started_at: string;
  ended_at?: string;
  last_status: number;
  last_error?: string;
  probe_count: number;
  duration_s: number;
}

export interface UptimeStats {
  range_start: string;
  range_end: string;
  total_seconds: number;
  down_seconds: number;
  percent: number;
  incident_count: number;
  mttr_seconds: number;
  longest_seconds: number;
}

export function getEndpointIncidents(_token: string, id: number, range: string) {
  return request<EndpointIncident[]>(`/endpoints/${id}/incidents?range=${range}`);
}

export function getEndpointUptime(_token: string, id: number, range: string) {
  return request<UptimeStats>(`/endpoints/${id}/uptime?range=${range}`);
}

export function listEndpoints(_token: string, agentId: string) {
  return request<Endpoint[]>(`/agents/${agentId}/endpoints`);
}

export function createEndpoint(_token: string, agentId: string, name: string, url: string) {
  return request<Endpoint>(`/agents/${agentId}/endpoints`, {
    method: 'POST',
    body: JSON.stringify({ name, url }),
  });
}

export function setEndpointAlert(_token: string, id: number, enabled: boolean) {
  return request<{ id: number; alert_on_down: boolean }>(`/endpoints/${id}/alert`, {
    method: 'PATCH',
    body: JSON.stringify({ enabled }),
  });
}

export function updateEndpoint(_token: string, id: number, name: string, url: string) {
  return request<Endpoint>(`/endpoints/${id}`, {
    method: 'PUT',
    body: JSON.stringify({ name, url }),
  });
}

export async function deleteEndpoint(_token: string, id: number): Promise<void> {
  const res = await fetch(`${API_BASE}/endpoints/${id}`, {
    method: 'DELETE',
    credentials: 'include',
  });
  if (res.status === 401) { clearSession(); throw new Error('Session expired'); }
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || 'Delete failed');
  }
}

export function getEndpointProbes(_token: string, id: number, minutes: number) {
  return request<EndpointProbe[]>(`/endpoints/${id}/probes?minutes=${minutes}`);
}

export interface EndpointSettings {
  probe_interval_seconds: number;
  incident_retention_days: number;
}

export function getEndpointSettings(_token: string) {
  return request<EndpointSettings>(`/settings/endpoints`);
}

export function saveEndpointSettings(_token: string, s: EndpointSettings) {
  return request<EndpointSettings>(`/settings/endpoints`, {
    method: 'PUT',
    body: JSON.stringify(s),
  });
}

// ── Webhooks ──────────────────────────────────────────────────────────────────

export type WebhookType = 'discord';

export interface Webhook {
  id: number;
  name: string;
  url: string;
  type: WebhookType;
  created_at: string;
}

export function listWebhooks(_token: string) {
  return request<Webhook[]>('/webhooks');
}

export function createWebhook(_token: string, name: string, url: string, type: WebhookType) {
  return request<Webhook>('/webhooks', {
    method: 'POST',
    body: JSON.stringify({ name, url, type }),
  });
}

export async function deleteWebhook(_token: string, id: number): Promise<void> {
  const res = await fetch(`${API_BASE}/webhooks/${id}`, {
    method: 'DELETE',
    credentials: 'include',
  });
  if (res.status === 401) { clearSession(); throw new Error('Session expired'); }
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || 'Delete failed');
  }
}

// ── Session ───────────────────────────────────────────────────────────────────
// The JWT lives in an httpOnly cookie set by the server — JS never sees it.
// localStorage holds only username+role for UI display purposes.

const USER_KEY = 'chowkidar_user';
const ROLE_KEY = 'chowkidar_role';

export function saveSession(username: string, role: Role) {
  localStorage.setItem(USER_KEY, username);
  localStorage.setItem(ROLE_KEY, role);
}

// getToken returns the stored username as a truthy "logged-in" indicator.
// All API functions accept it as a parameter for signature compatibility but
// do not use it for Authorization headers — the httpOnly cookie handles auth.
export function getToken(): string | null {
  return localStorage.getItem(USER_KEY);
}

export function getStoredRole(): Role | null {
  return localStorage.getItem(ROLE_KEY) as Role | null;
}

export function clearSession() {
  localStorage.removeItem(USER_KEY);
  localStorage.removeItem(ROLE_KEY);
  // Fire-and-forget — clears the httpOnly session cookie server-side.
  fetch(`${API_BASE}/auth/logout`, { method: 'POST', credentials: 'include' }).catch(() => {});
}

// ── Persisted alerts + retention setting ──────────────────────────────────────

export interface RecentAlertsResp {
  events: PersistedAlertEvent[];
  unread: number;
}

export function recentAlerts(limit = 100) {
  return request<RecentAlertsResp>(`/alerts/recent?limit=${limit}`);
}

export async function markAlertsSeen(): Promise<void> {
  const res = await fetch(`${API_BASE}/alerts/seen`, {
    method: 'POST',
    credentials: 'include',
  });
  if (res.status === 401) { clearSession(); throw new Error('Session expired'); }
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || 'Failed to mark alerts seen');
  }
}

export function getAlertRetention() {
  return request<{ days: number }>('/settings/alert-retention');
}

export function setAlertRetention(days: number) {
  return request<{ days: number }>('/settings/alert-retention', {
    method: 'PUT',
    body: JSON.stringify({ days }),
  });
}
