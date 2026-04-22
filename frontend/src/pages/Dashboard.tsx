import { useState, useEffect, useCallback } from 'react';
import { clearSession, getToken, listAgents, registerAgent, type Agent } from '../api/client';
import AgentDetail from '../components/AgentDetail';

type AgentStatus = 'pending' | 'online' | 'offline';

// pending = never reported | online = reported within 35 s | offline = was online, now silent
function agentStatus(lastSeen: string | null): AgentStatus {
  if (!lastSeen) return 'pending';
  return Date.now() - new Date(lastSeen).getTime() < 35_000 ? 'online' : 'offline';
}

const STATUS_LABEL: Record<AgentStatus, string> = {
  pending: 'Pending',
  online:  'Online',
  offline: 'Offline',
};

function pct(used: number | null, total: number | null): number {
  if (!used || !total || total === 0) return 0;
  return Math.round((used / total) * 100);
}

function fmtGB(gb: number | null): string {
  if (gb == null) return '—';
  return gb >= 1 ? `${gb.toFixed(1)} GB` : `${(gb * 1024).toFixed(0)} MB`;
}

export default function Dashboard({ username, onLogout }: { username: string; onLogout: () => void }) {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [loading, setLoading] = useState(true);
  const [selectedAgent, setSelectedAgent] = useState<Agent | null>(null);

  const [showRegister, setShowRegister] = useState(false);
  const [newHostname, setNewHostname] = useState('');
  const [registering, setRegistering] = useState(false);
  const [registerError, setRegisterError] = useState('');

  const [newAgent, setNewAgent] = useState<{ agent_id: string; token: string } | null>(null);
  const [copied, setCopied] = useState(false);

  const loadAgents = useCallback(async () => {
    const token = getToken();
    if (!token) { onLogout(); return; }
    try {
      const data = await listAgents(token);
      setAgents(data ?? []);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') onLogout();
    } finally {
      setLoading(false);
    }
  }, [onLogout]);

  useEffect(() => {
    loadAgents();
    const id = setInterval(loadAgents, 10_000);
    return () => clearInterval(id);
  }, [loadAgents]);

  const handleLogout = () => { clearSession(); onLogout(); };

  const handleRegister = async (e: React.FormEvent) => {
    e.preventDefault();
    setRegisterError('');
    setRegistering(true);
    try {
      const token = getToken();
      if (!token) { onLogout(); return; }
      const result = await registerAgent(token, newHostname.trim());
      setNewAgent(result);
      setNewHostname('');
      setShowRegister(false);
      await loadAgents();
    } catch (err) {
      setRegisterError(err instanceof Error ? err.message : 'Registration failed');
    } finally {
      setRegistering(false);
    }
  };

  const copyToken = () => {
    if (!newAgent) return;
    navigator.clipboard.writeText(newAgent.token).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  };

  // Show detail view when an agent is selected.
  if (selectedAgent) {
    return (
      <AgentDetail
        agent={selectedAgent}
        token={getToken()!}
        onBack={() => setSelectedAgent(null)}
        onExpired={onLogout}
      />
    );
  }

  return (
    <div className="dashboard">
      <header className="dash-header">
        <div className="dash-brand">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
            <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
          </svg>
          <span className="dash-title">Chowkidar</span>
        </div>
        <div className="dash-user">
          <span>{username}</span>
          <button className="btn-ghost" onClick={handleLogout}>Sign out</button>
        </div>
      </header>

      <main className="dash-main">
        <div className="dash-section">
          <div className="dash-section-header">
            <h2 className="dash-section-title">Agents</h2>
            <button className="btn-primary" onClick={() => setShowRegister(true)}>+ Add Agent</button>
          </div>

          {loading ? (
            <div className="dash-loading">Loading agents...</div>
          ) : agents.length === 0 ? (
            <div className="dash-empty">No agents registered. Click "Add Agent" to get started.</div>
          ) : (
            <div className="agents-grid">
              {agents.map(agent => {
                const status = agentStatus(agent.last_seen);
                const cpuPct = agent.cpu_percent ?? 0;
                const memPct = pct(agent.mem_used_gb, agent.mem_total_gb);
                const diskPct = pct(agent.disk_used_gb, agent.disk_total_gb);
                const hasMetrics = agent.cpu_percent != null;

                return (
                  <button
                    key={agent.id}
                    className="agent-card"
                    onClick={() => setSelectedAgent(agent)}
                  >
                    <div className="agent-card-header">
                      <span className="agent-hostname">{agent.hostname}</span>
                      <span className={`agent-status ${status}`}>
                        {STATUS_LABEL[status]}
                      </span>
                    </div>

                    {hasMetrics ? (
                      <div className="agent-metrics">
                        <div className="metric-row">
                          <span className="metric-label">CPU</span>
                          <div className="metric-bar-wrap">
                            <div className="metric-bar" style={{ width: `${cpuPct}%`, background: barColor(cpuPct) }} />
                          </div>
                          <span className="metric-value">{cpuPct.toFixed(1)}%</span>
                        </div>
                        <div className="metric-row">
                          <span className="metric-label">MEM</span>
                          <div className="metric-bar-wrap">
                            <div className="metric-bar" style={{ width: `${memPct}%`, background: barColor(memPct) }} />
                          </div>
                          <span className="metric-value">{fmtGB(agent.mem_used_gb)} / {fmtGB(agent.mem_total_gb)}</span>
                        </div>
                        <div className="metric-row">
                          <span className="metric-label">DISK</span>
                          <div className="metric-bar-wrap">
                            <div className="metric-bar" style={{ width: `${diskPct}%`, background: barColor(diskPct) }} />
                          </div>
                          <span className="metric-value">{fmtGB(agent.disk_used_gb)} / {fmtGB(agent.disk_total_gb)}</span>
                        </div>
                        <div className="agent-footer">
                          <span>{agent.container_count} container{agent.container_count !== 1 ? 's' : ''}</span>
                          {agent.last_seen && (
                            <span>seen {timeAgo(agent.last_seen)}</span>
                          )}
                        </div>
                      </div>
                    ) : (
                      <div className="agent-no-data">
                        {status === 'pending'
                          ? 'Waiting for agent to connect…'
                          : 'No recent data'}
                      </div>
                    )}
                  </button>
                );
              })}
            </div>
          )}
        </div>

        {/* Token display after successful registration */}
        {newAgent && (
          <div className="modal-overlay" onClick={() => setNewAgent(null)}>
            <div className="modal" onClick={e => e.stopPropagation()}>
              <h3 className="modal-title">Agent Registered</h3>
              <p className="modal-text">Copy this token now — it won't be shown again.</p>
              <div className="token-display">
                <code>{newAgent.token}</code>
                <button className="btn-secondary" onClick={copyToken}>
                  {copied ? 'Copied!' : 'Copy'}
                </button>
              </div>
              <button className="btn-primary modal-close" onClick={() => setNewAgent(null)}>Done</button>
            </div>
          </div>
        )}

        {showRegister && (
          <div className="modal-overlay" onClick={() => setShowRegister(false)}>
            <div className="modal" onClick={e => e.stopPropagation()}>
              <h3 className="modal-title">Register New Agent</h3>
              {registerError && <div className="login-error" style={{ marginBottom: 12 }}>{registerError}</div>}
              <form onSubmit={handleRegister}>
                <label className="form-label">
                  Hostname
                  <input
                    type="text"
                    className="form-input"
                    value={newHostname}
                    onChange={e => setNewHostname(e.target.value)}
                    placeholder="e.g., production-server-01"
                    required
                  />
                </label>
                <div className="modal-actions">
                  <button type="button" className="btn-secondary" onClick={() => setShowRegister(false)}>Cancel</button>
                  <button type="submit" className="btn-primary" disabled={registering}>
                    {registering ? 'Registering...' : 'Register'}
                  </button>
                </div>
              </form>
            </div>
          </div>
        )}
      </main>
    </div>
  );
}

// Green → yellow → red based on usage percentage.
function barColor(pct: number): string {
  if (pct < 60) return '#22c55e';
  if (pct < 85) return '#f59e0b';
  return '#ef4444';
}

function timeAgo(iso: string): string {
  const secs = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
  if (secs < 60) return `${secs}s ago`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m ago`;
  return `${Math.floor(secs / 3600)}h ago`;
}
