import { describe, it, expect, vi, afterEach } from 'vitest';
import { login, listAgents } from './client';

function mockFetch(status: number, body: unknown) {
  return vi.fn().mockResolvedValue({
    status,
    ok: status >= 200 && status < 300,
    json: () => Promise.resolve(body),
    statusText: String(status),
  });
}

describe('login', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('returns token on 200', async () => {
    vi.stubGlobal('fetch', mockFetch(200, { token: 'tok123', username: 'admin', role: 'admin' }));
    const res = await login('admin', 'pass');
    expect(res.token).toBe('tok123');
    expect(res.username).toBe('admin');
  });

  it('throws on 401', async () => {
    vi.stubGlobal('fetch', mockFetch(401, { error: 'invalid credentials' }));
    // 401 clears session and throws "Session expired"
    await expect(login('admin', 'wrong')).rejects.toThrow();
  });
});

describe('request Authorization header', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('listAgents attaches Authorization header', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      status: 200,
      ok: true,
      json: () => Promise.resolve([]),
    });
    vi.stubGlobal('fetch', fetchMock);
    await listAgents('my-token');
    const callArgs = fetchMock.mock.calls[0];
    const options = callArgs[1] as RequestInit;
    expect((options.headers as Record<string, string>)['Authorization']).toBe('Bearer my-token');
  });

  it('throws Session expired on 401 from request', async () => {
    vi.stubGlobal('fetch', mockFetch(401, { error: 'unauthorized' }));
    await expect(listAgents('expired-token')).rejects.toThrow('Session expired');
  });

  it('listAgents returns agent array', async () => {
    const agents = [
      { id: 'a1', hostname: 'host1', last_seen: null, cpu_percent: null,
        mem_used_gb: null, mem_total_gb: null, disk_used_gb: null,
        disk_total_gb: null, container_count: 0, alerts_enabled: false, active_issues: 0 },
    ];
    vi.stubGlobal('fetch', mockFetch(200, agents));
    const result = await listAgents('tok');
    expect(result).toHaveLength(1);
    expect(result[0].hostname).toBe('host1');
  });
});
