import { useCallback, useEffect, useState } from 'react';
import { useNavigate, useOutletContext, useParams } from 'react-router-dom';
import { getAgent, type Agent } from '../api/client';
import AgentDetail, { type AgentTab } from '../components/AgentDetail';
import type { AuthCtx } from './Protected';

const TABS: readonly AgentTab[] = ['overview', 'alerts', 'endpoints', 'settings'];

function parseTab(raw: string | undefined): AgentTab {
  return (TABS as readonly string[]).includes(raw ?? '') ? (raw as AgentTab) : 'overview';
}

export default function AgentDetailPage() {
  const { id = '', tab: rawTab } = useParams<{ id: string; tab: string }>();
  const tab = parseTab(rawTab);

  const { token, role, onLogout } = useOutletContext<AuthCtx>();
  const navigate = useNavigate();

  const [agent, setAgent] = useState<Agent | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    getAgent(token, id)
      .then(a => { if (!cancelled) setAgent(a); })
      .catch(err => {
        if (cancelled) return;
        if (err instanceof Error && err.message === 'Session expired') {
          onLogout();
          return;
        }
        setError(err instanceof Error ? err.message : 'Failed to load agent');
      })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [id, token, onLogout]);

  const onTabChange = useCallback((t: AgentTab) => {
    navigate(`/agents/${id}/${t}`);
  }, [navigate, id]);

  // Developers can only view the Overview tab; force-redirect anything else.
  useEffect(() => {
    if (role === 'developer' && tab !== 'overview') {
      navigate(`/agents/${id}/overview`, { replace: true });
    }
  }, [role, tab, id, navigate]);

  if (loading) return <div className="dash-loading" style={{ padding: 48 }}>Loading agent…</div>;
  if (error || !agent) {
    return (
      <main className="dash-main">
        <div className="dash-section">
          <div className="dash-empty">{error ?? 'Agent not found.'}</div>
          <button className="btn-secondary" style={{ marginTop: 16 }} onClick={() => navigate('/agents')}>
            ← Back to agents
          </button>
        </div>
      </main>
    );
  }

  return (
    <AgentDetail
      key={agent.id}
      agent={agent}
      token={token}
      role={role}
      tab={tab}
      onTabChange={onTabChange}
      onBack={() => navigate('/agents')}
      onExpired={onLogout}
      onNavigateSettings={() => navigate('/settings')}
    />
  );
}
