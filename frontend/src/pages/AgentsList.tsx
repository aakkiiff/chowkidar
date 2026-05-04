import { useCallback, useEffect, useState } from 'react';
import { useNavigate, useOutletContext } from 'react-router-dom';
import { listAgents, registerAgent, type Agent } from '../api/client';
import type { AuthCtx } from './Protected';

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

function barColor(p: number): string {
  if (p < 60) return '#73bf69';
  if (p < 85) return '#f2cc0c';
  return '#e0626a';
}

function timeAgo(iso: string): string {
  const secs = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
  if (secs < 60) return `${secs}s ago`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m ago`;
  return `${Math.floor(secs / 3600)}h ago`;
}

export default function AgentsList() {
  const { token, role, onLogout } = useOutletContext<AuthCtx>();
  const isAdmin = role === 'admin';
  const navigate = useNavigate();

  const [agents, setAgents] = useState<Agent[]>([]);
  const [loading, setLoading] = useState(true);
  const [pollError, setPollError] = useState(false);

  const [showRegister, setShowRegister] = useState(false);
  const [newHostname, setNewHostname] = useState('');
  const [registering, setRegistering] = useState(false);
  const [registerError, setRegisterError] = useState('');

  const [newAgent, setNewAgent] = useState<{ agent_id: string; token: string } | null>(null);
  const [copied, setCopied] = useState(false);

  const pollDelay = useCallback((hadError: boolean) => hadError ? 30_000 : 10_000, []);

  const loadAgents = useCallback(async () => {
    try {
      const data = await listAgents(token);
      setAgents(data ?? []);
      setPollError(false);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') { onLogout(); return; }
      setPollError(true);
    } finally {
      setLoading(false);
    }
  }, [token, onLogout]);

  useEffect(() => {
    let id: ReturnType<typeof setTimeout>;
    const schedule = (error: boolean) => {
      id = setTimeout(async () => {
        await loadAgents();
        schedule(pollError);
      }, pollDelay(error));
    };
    loadAgents();
    schedule(false);
    return () => clearTimeout(id);
  }, [loadAgents, pollDelay, pollError]);

  const handleRegister = async (e: React.FormEvent) => {
    e.preventDefault();
    setRegisterError('');
    setRegistering(true);
    try {
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

  const openAgent = (a: Agent) => navigate(`/agents/${a.id}/overview`);

  return (
    <main className="dash-main">
      <div className="dash-section">
        <div className="dash-section-header">
          <h2 className="dash-section-title">Agents</h2>
          {isAdmin && (
            <button className="btn-primary" onClick={() => setShowRegister(true)}>+ Add Agent</button>
          )}
        </div>

        {loading ? (
          <div className="dash-loading">Loading agents…</div>
        ) : agents.length === 0 ? (
          <div className="dash-empty">
            {isAdmin
              ? 'No agents registered. Click "Add Agent" to get started.'
              : 'No agents assigned to your account. Contact an administrator.'}
          </div>
        ) : (
          <div className="agents-grid">
            {agents.map(agent => {
              const status = agentStatus(agent.last_seen);
              const cpuPct = agent.cpu_percent ?? 0;
              const memPct = pct(agent.mem_used_gb, agent.mem_total_gb);
              const diskPct = pct(agent.disk_used_gb, agent.disk_total_gb);
              const hasMetrics = agent.cpu_percent != null;
              const alertsOn = agent.alerts_enabled;
              const issues = agent.active_issues ?? 0;
              return (
                <div
                  key={agent.id}
                  className={`agent-card status-${status}`}
                  role="button"
                  tabIndex={0}
                  onClick={() => openAgent(agent)}
                  onKeyDown={e => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      openAgent(agent);
                    }
                  }}
                >
                  <div className="agent-card-header">
                    <span className="agent-hostname">{agent.hostname}</span>
                    <div className="agent-card-header-right">
                      {issues > 0 && (
                        <span className="issue-badge" title={`${issues} active issue${issues !== 1 ? 's' : ''}`}>
                          {issues}
                        </span>
                      )}
                      <span
                        className={`alert-bell${alertsOn ? ' on' : ''}`}
                        aria-label={alertsOn ? 'Alerts enabled' : 'Alerts disabled'}
                        title={alertsOn ? 'Alerts ON — click card to manage' : 'Alerts OFF — click card to manage'}
                      >
                        <svg width="14" height="14" viewBox="0 0 24 24" fill={alertsOn ? 'currentColor' : 'none'} stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                          <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
                          <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
                        </svg>
                      </span>
                      <span className={`agent-status ${status}`}>{STATUS_LABEL[status]}</span>
                    </div>
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
                        {agent.last_seen && <span>seen {timeAgo(agent.last_seen)}</span>}
                      </div>
                    </div>
                  ) : (
                    <div className="agent-no-data">
                      {status === 'pending' ? 'Waiting for agent to connect…' : 'No recent data'}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>

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
                  {registering ? 'Registering…' : 'Register'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </main>
  );
}
