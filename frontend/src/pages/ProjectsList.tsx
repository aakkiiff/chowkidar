import { useCallback, useEffect, useState } from 'react';
import { useNavigate, useOutletContext } from 'react-router-dom';
import { listProjects, createProject, type Project } from '../api/client';
import type { AuthCtx } from './Protected';

export default function ProjectsList() {
  const { role, onLogout } = useOutletContext<AuthCtx>();
  const isAdmin = role === 'admin';
  const navigate = useNavigate();

  const [projects, setProjects] = useState<Project[]>([]);
  const [loading, setLoading] = useState(true);

  const [showCreate, setShowCreate] = useState(false);
  const [newName, setNewName] = useState('');
  const [newEnv, setNewEnv] = useState('');
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    try {
      const data = await listProjects();
      setProjects(data ?? []);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') { onLogout(); return; }
    } finally {
      setLoading(false);
    }
  }, [onLogout]);

  useEffect(() => { load(); }, [load]);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setCreating(true);
    try {
      await createProject(newName.trim(), newEnv.trim());
      setNewName('');
      setNewEnv('');
      setShowCreate(false);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Create failed');
    } finally {
      setCreating(false);
    }
  };

  const open = (p: Project) => navigate(`/projects/${p.id}`);

  return (
    <main className="dash-main">
      <div className="dash-section">
        <div className="dash-section-header">
          <h2 className="dash-section-title">Projects</h2>
          {isAdmin && (
            <button className="btn-primary" onClick={() => setShowCreate(true)}>+ New Project</button>
          )}
        </div>

        {loading ? (
          <div className="dash-loading">Loading projects…</div>
        ) : projects.length === 0 ? (
          <div className="dash-empty">
            {isAdmin
              ? 'No projects yet. Click "New Project" to create one.'
              : 'No projects available.'}
          </div>
        ) : (
          <div className="agents-grid">
            {projects.map(p => (
              <div
                key={p.id}
                className="agent-card status-online"
                role="button"
                tabIndex={0}
                onClick={() => open(p)}
                onKeyDown={e => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    open(p);
                  }
                }}
              >
                <div className="agent-card-header">
                  <span className="agent-hostname">{p.name}</span>
                  {p.environment && (
                    <span className="agent-status online">{p.environment}</span>
                  )}
                </div>
                <div className="agent-no-data">
                  {p.agent_count} agent{p.agent_count !== 1 ? 's' : ''}
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {showCreate && (
        <div className="modal-overlay" onClick={() => setShowCreate(false)}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <h3 className="modal-title">New Project</h3>
            {error && <div className="login-error" style={{ marginBottom: 12 }}>{error}</div>}
            <form onSubmit={handleCreate}>
              <label className="form-label">
                Name
                <input
                  type="text"
                  className="form-input"
                  value={newName}
                  onChange={e => setNewName(e.target.value)}
                  placeholder="e.g., backend-services"
                  required
                  maxLength={64}
                />
              </label>
              <label className="form-label">
                Environment
                <input
                  type="text"
                  className="form-input"
                  value={newEnv}
                  onChange={e => setNewEnv(e.target.value)}
                  placeholder="e.g., prod, staging, dev (optional)"
                  maxLength={32}
                />
              </label>
              <div className="modal-actions">
                <button type="button" className="btn-secondary" onClick={() => setShowCreate(false)}>Cancel</button>
                <button type="submit" className="btn-primary" disabled={creating}>
                  {creating ? 'Creating…' : 'Create'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </main>
  );
}
