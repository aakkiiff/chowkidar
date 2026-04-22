const API_BASE = '/api/v1';

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

export function login(username: string, password: string) {
  return request<{ token: string; username: string }>('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  });
}

export function me(token: string) {
  return request<{ username: string }>('/auth/me', {
    headers: { Authorization: `Bearer ${token}` },
  });
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
}

export function listAgents(token: string) {
  return request<Agent[]>('/agents', {
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

// ── Agent detail ──────────────────────────────────────────────────────────────

export interface Container {
  id: string;
  name: string;
  image: string;
  status: string;
  cpu_percent: number;
  mem_used_mb: number;
  mem_limit_mb: number;
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

export type HistoryRange = '1h' | '6h' | '24h' | '7d';

export function getContainerHistory(
  token: string,
  agentId: string,
  name: string,
  range: HistoryRange,
) {
  return request<{ points: ContainerPoint[] }>(
    `/agents/${agentId}/containers/${encodeURIComponent(name)}/history?range=${range}`,
    { headers: { Authorization: `Bearer ${token}` } },
  );
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
