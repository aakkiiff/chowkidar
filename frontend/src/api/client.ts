// Defaults to same-origin path — works with Vite dev proxy and nginx reverse
// proxy. Override with VITE_API_BASE when hosting frontend and server on
// different origins.
const API_BASE = (import.meta.env.VITE_API_BASE ?? '/api/v1').replace(/\/$/, '');

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    headers: { 'Content-Type': 'application/json', ...options?.headers },
    ...options,
  });

  if (res.status === 401) {
    clearSession();
    throw new Error('Session expired');
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || 'Request failed');
  }
  return res.json();
}

// ── Auth ──────────────────────────────────────────────────────────────────────

export type Role = 'admin' | 'developer';

export function login(username: string, password: string) {
  return request<{ token: string; username: string; role: Role }>('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  });
}

export function me(token: string) {
  return request<{ username: string; role: Role }>('/auth/me', {
    headers: { Authorization: `Bearer ${token}` },
  });
}

// ── User management (admin only) ──────────────────────────────────────────────

export interface AppUser {
  id: number;
  username: string;
  role: Role;
  agent_ids?: string[];
  created_at: string;
}

export function listUsers(token: string) {
  return request<AppUser[]>('/users', {
    headers: { Authorization: `Bearer ${token}` },
  });
}

export function createUser(token: string, username: string, password: string, role: Role, agentIds?: string[]) {
  return request<AppUser>('/users', {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
    body: JSON.stringify({ username, password, role, agent_ids: agentIds }),
  });
}

export async function deleteUser(token: string, id: number): Promise<void> {
  const res = await fetch(`${API_BASE}/users/${id}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  });
  if (res.status === 401) {
    clearSession();
    throw new Error('Session expired');
  }
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || 'Delete failed');
  }
}

export async function setUserPassword(token: string, id: number, password: string): Promise<void> {
  const res = await fetch(`${API_BASE}/users/${id}/password`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ password }),
  });
  if (res.status === 401) {
    clearSession();
    throw new Error('Session expired');
  }
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || 'Update failed');
  }
}

export async function changeOwnPassword(token: string, currentPassword: string, newPassword: string): Promise<void> {
  const res = await fetch(`${API_BASE}/auth/password`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
  });
  if (res.status === 401) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    // 401 from this endpoint means wrong current password, not session expiry
    throw new Error(body.error || 'Unauthorized');
  }
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || 'Update failed');
  }
}

export async function setUserAgents(token: string, userId: number, agentIds: string[]): Promise<void> {
  const res = await fetch(`${API_BASE}/users/${userId}/agents`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ agent_ids: agentIds }),
  });
  if (res.status === 401) {
    clearSession();
    throw new Error('Session expired');
  }
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || 'Update failed');
  }
}

// ── Agents ────────────────────────────────────────────────────────────────────

// Matches the server's agentResponse — metrics are null until first report.
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
}

export function listAgents(token: string) {
  return request<Agent[]>('/agents', {
    headers: { Authorization: `Bearer ${token}` },
  });
}

export function getAgent(token: string, agentId: string) {
  return request<Agent>(`/agents/${agentId}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
}

export function registerAgent(token: string, hostname: string) {
  return request<{ agent_id: string; token: string }>('/agents/register', {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
    body: JSON.stringify({ hostname }),
  });
}

export interface AlertRule {
  agent_id: string;
  // System (host) thresholds — % values
  cpu_enabled: boolean;
  cpu_threshold: number;
  mem_enabled: boolean;
  mem_threshold: number;
  disk_enabled: boolean;
  disk_threshold: number;
  // Container thresholds. Apply to every container under this agent.
  ctr_down_enabled: boolean;
  ctr_cpu_enabled: boolean;
  ctr_cpu_threshold_mcore: number; // 1000 mCore = 1 full core
  ctr_mem_enabled: boolean;
  ctr_mem_threshold: number; // % of mem limit
  // Master toggle: enable/disable down alerts for all endpoints under this agent.
  endpoint_down_enabled: boolean;
  // Fires when any endpoint's TLS leaf cert is within 14 days of expiry.
  ssl_alert_enabled: boolean;
  // Fires when the agent stops reporting beyond the sustain window.
  agent_down_enabled: boolean;
  webhook_id: number | null;
}

export function getAlertRule(token: string, agentId: string) {
  return request<AlertRule>(`/agents/${agentId}/alert-rule`, {
    headers: { Authorization: `Bearer ${token}` },
  });
}

export interface AlertSettings {
  sustain_seconds: number;
  resend_cooldown_seconds: number;
}

export function getAlertSettings(token: string) {
  return request<AlertSettings>('/settings/alerts', {
    headers: { Authorization: `Bearer ${token}` },
  });
}

export function saveAlertSettings(token: string, s: AlertSettings) {
  return request<AlertSettings>('/settings/alerts', {
    method: 'PUT',
    headers: { Authorization: `Bearer ${token}` },
    body: JSON.stringify(s),
  });
}

export function saveAlertRule(token: string, rule: AlertRule) {
  return request<AlertRule>(`/agents/${rule.agent_id}/alert-rule`, {
    method: 'PUT',
    headers: { Authorization: `Bearer ${token}` },
    body: JSON.stringify(rule),
  });
}

export function setAgentAlerts(token: string, agentId: string, enabled: boolean) {
  return request<{ id: string; alerts_enabled: boolean }>(`/agents/${agentId}/alerts`, {
    method: 'PATCH',
    headers: { Authorization: `Bearer ${token}` },
    body: JSON.stringify({ enabled }),
  });
}

export function renameAgent(token: string, agentId: string, hostname: string) {
  return request<{ id: string; hostname: string }>(`/agents/${agentId}`, {
    method: 'PATCH',
    headers: { Authorization: `Bearer ${token}` },
    body: JSON.stringify({ hostname }),
  });
}

export async function deleteAgent(token: string, agentId: string): Promise<void> {
  const res = await fetch(`${API_BASE}/agents/${agentId}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  });
  if (res.status === 401) {
    clearSession();
    throw new Error('Session expired');
  }
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

export function getAgentContainers(token: string, agentId: string) {
  return request<Container[]>(`/agents/${agentId}/containers`, {
    headers: { Authorization: `Bearer ${token}` },
  });
}

export interface ContainerPoint {
  timestamp: string;
  cpu_percent: number;
  mem_used_mb: number;
  mem_limit_mb: number;
}

// Historical range expressed in minutes. Server accepts any positive integer
// up to 30 days. UI keeps a preset list but also allows custom values.
export type HistoryRange = number;

export function getContainerHistory(
  token: string,
  agentId: string,
  name: string,
  minutes: HistoryRange,
) {
  return request<{ points: ContainerPoint[] }>(
    `/agents/${agentId}/containers/${encodeURIComponent(name)}/history?range=${minutes}`,
    { headers: { Authorization: `Bearer ${token}` } },
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

// streamAlerts opens an SSE connection and reconnects with exponential backoff
// if the stream drops. It resolves only when the AbortController is triggered.
export function streamAlerts(
  token: string,
  onEvent: (e: AlertEvent) => void,
  onError: (err: unknown) => void,
): AbortController {
  const ctrl = new AbortController();

  const connect = async (): Promise<'retry' | 'stop'> => {
    try {
      const res = await fetch(`${API_BASE}/alerts/stream`, {
        headers: { Authorization: `Bearer ${token}` },
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
        if (done) return 'retry'; // server closed — reconnect
        buf += value;
        let idx: number;
        while ((idx = buf.indexOf('\n\n')) >= 0) {
          const frame = buf.slice(0, idx);
          buf = buf.slice(idx + 2);
          if (frame.startsWith(':')) continue;
          const dataLine = frame.split('\n').find(l => l.startsWith('data: '));
          if (!dataLine) continue;
          try {
            onEvent(JSON.parse(dataLine.slice(6)) as AlertEvent);
          } catch {
            /* ignore malformed frame */
          }
        }
      }
    } catch (err) {
      if ((err as Error).name === 'AbortError') return 'stop';
      // Surface transient errors but keep trying.
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
  token: string,
  agentId: string,
  name: string,
  minutes: number,
) {
  return request<LogLine[]>(
    `/agents/${agentId}/containers/${encodeURIComponent(name)}/logs?minutes=${minutes}`,
    { headers: { Authorization: `Bearer ${token}` } },
  );
}

export interface LogLine {
  container_id: string;
  container_name: string;
  stream: 'stdout' | 'stderr';
  timestamp: string;
  text: string;
}

// streamLogs opens an SSE connection to the log tail endpoint, authed with the
// bearer token. onLine is called for each incoming event; the returned AbortController
// can be used to close the stream (and is triggered automatically by the signal).
export function streamLogs(
  token: string,
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
        headers: { Authorization: `Bearer ${token}` },
        signal: ctrl.signal,
      });
      if (res.status === 401) {
        clearSession();
        throw new Error('Session expired');
      }
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
          // skip heartbeat comments (": ping")
          if (frame.startsWith(':')) continue;
          const dataLine = frame.split('\n').find(l => l.startsWith('data: '));
          if (!dataLine) continue;
          try {
            onLine(JSON.parse(dataLine.slice(6)) as LogLine);
          } catch {
            /* ignore malformed frame */
          }
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
  /** Server cert NotAfter from latest TLS probe. null for plain http. */
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
  ended_at?: string;       // omitted on ongoing incidents
  last_status: number;
  last_error?: string;
  probe_count: number;
  duration_s: number;       // seconds; for ongoing rows == elapsed since start
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

export function getEndpointIncidents(token: string, id: number, range: string) {
  return request<EndpointIncident[]>(`/endpoints/${id}/incidents?range=${range}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
}

export function getEndpointUptime(token: string, id: number, range: string) {
  return request<UptimeStats>(`/endpoints/${id}/uptime?range=${range}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
}

export function listEndpoints(token: string, agentId: string) {
  return request<Endpoint[]>(`/agents/${agentId}/endpoints`, {
    headers: { Authorization: `Bearer ${token}` },
  });
}

export function createEndpoint(token: string, agentId: string, name: string, url: string) {
  return request<Endpoint>(`/agents/${agentId}/endpoints`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
    body: JSON.stringify({ name, url }),
  });
}

export function setEndpointAlert(token: string, id: number, enabled: boolean) {
  return request<{ id: number; alert_on_down: boolean }>(`/endpoints/${id}/alert`, {
    method: 'PATCH',
    headers: { Authorization: `Bearer ${token}` },
    body: JSON.stringify({ enabled }),
  });
}

export function updateEndpoint(token: string, id: number, name: string, url: string) {
  return request<Endpoint>(`/endpoints/${id}`, {
    method: 'PUT',
    headers: { Authorization: `Bearer ${token}` },
    body: JSON.stringify({ name, url }),
  });
}

export async function deleteEndpoint(token: string, id: number): Promise<void> {
  const res = await fetch(`${API_BASE}/endpoints/${id}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  });
  if (res.status === 401) {
    clearSession();
    throw new Error('Session expired');
  }
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || 'Delete failed');
  }
}

export function getEndpointProbes(token: string, id: number, minutes: number) {
  return request<EndpointProbe[]>(`/endpoints/${id}/probes?minutes=${minutes}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
}

export interface EndpointSettings {
  probe_interval_seconds: number;
  incident_retention_days: number;
}

export function getEndpointSettings(token: string) {
  return request<EndpointSettings>(`/settings/endpoints`, {
    headers: { Authorization: `Bearer ${token}` },
  });
}

export function saveEndpointSettings(token: string, s: EndpointSettings) {
  return request<EndpointSettings>(`/settings/endpoints`, {
    method: 'PUT',
    headers: { Authorization: `Bearer ${token}` },
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

export function listWebhooks(token: string) {
  return request<Webhook[]>('/webhooks', {
    headers: { Authorization: `Bearer ${token}` },
  });
}

export function createWebhook(token: string, name: string, url: string, type: WebhookType) {
  return request<Webhook>('/webhooks', {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
    body: JSON.stringify({ name, url, type }),
  });
}

export async function deleteWebhook(token: string, id: number): Promise<void> {
  const res = await fetch(`${API_BASE}/webhooks/${id}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` },
  });
  if (res.status === 401) {
    clearSession();
    throw new Error('Session expired');
  }
  if (!res.ok && res.status !== 204) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || 'Delete failed');
  }
}

// ── Session ───────────────────────────────────────────────────────────────────

const TOKEN_KEY = 'chowkidar_token';

export function saveSession(token: string) {
  localStorage.setItem(TOKEN_KEY, token);
}

export function getToken() {
  return localStorage.getItem(TOKEN_KEY);
}

export function clearSession() {
  localStorage.removeItem(TOKEN_KEY);
}
