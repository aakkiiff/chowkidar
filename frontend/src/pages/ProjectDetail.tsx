import { useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { getProject, updateProject, deleteProject, type Project } from '../api/client';
import AgentsList from './AgentsList';

export default function ProjectDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const projectId = Number(id);

  const [project, setProject] = useState<Project | null>(null);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState('');
  const [environment, setEnvironment] = useState('');
  const [error, setError] = useState('');

  useEffect(() => {
    if (!projectId || isNaN(projectId)) {
      navigate('/projects', { replace: true });
      return;
    }
    getProject(projectId)
      .then(p => {
        setProject(p);
        setName(p.name);
        setEnvironment(p.environment);
      })
      .catch(() => navigate('/projects', { replace: true }))
      .finally(() => setLoading(false));
  }, [projectId, navigate]);

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    try {
      const updated = await updateProject(projectId, name.trim(), environment.trim());
      setProject(updated);
      setEditing(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Update failed');
    }
  };

  const handleDelete = async () => {
    if (!confirm('Delete this project? Only allowed if no agents belong to it.')) return;
    try {
      await deleteProject(projectId);
      navigate('/projects', { replace: true });
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Delete failed');
    }
  };

  if (loading) return <main className="dash-main"><div className="dash-loading">Loading…</div></main>;
  if (!project) return null;

  const titleSuffix = project.environment ? ` (${project.environment})` : '';

  return (
    <main className="dash-main">
      <div className="dash-section">
        <div className="dash-section-header">
          <div>
            <button className="btn-secondary" onClick={() => navigate('/projects')} style={{ marginRight: 12 }}>
              ← Projects
            </button>
            <span style={{ fontSize: 18, fontWeight: 600 }}>{project.name}{titleSuffix}</span>
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <button className="btn-secondary" onClick={() => setEditing(true)}>Edit</button>
            <button className="btn-secondary" onClick={handleDelete}>Delete</button>
          </div>
        </div>
      </div>

      <AgentsList projectId={projectId} title="Agents in this project" />

      {editing && (
        <div className="modal-overlay" onClick={() => setEditing(false)}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <h3 className="modal-title">Edit Project</h3>
            {error && <div className="login-error" style={{ marginBottom: 12 }}>{error}</div>}
            <form onSubmit={handleSave}>
              <label className="form-label">
                Name
                <input
                  type="text"
                  className="form-input"
                  value={name}
                  onChange={e => setName(e.target.value)}
                  required
                  maxLength={64}
                />
              </label>
              <label className="form-label">
                Environment
                <input
                  type="text"
                  className="form-input"
                  value={environment}
                  onChange={e => setEnvironment(e.target.value)}
                  maxLength={32}
                />
              </label>
              <div className="modal-actions">
                <button type="button" className="btn-secondary" onClick={() => setEditing(false)}>Cancel</button>
                <button type="submit" className="btn-primary">Save</button>
              </div>
            </form>
          </div>
        </div>
      )}
    </main>
  );
}
