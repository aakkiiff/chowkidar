import { useCallback, useEffect, useState } from 'react';
import {
  listEndpoints,
  createEndpoint,
  updateEndpoint,
  deleteEndpoint,
  getEndpointProbes,
  type Endpoint,
  type EndpointProbe,
} from '../api/client';

const HEARTBEAT_BARS = 30;
const REFRESH_MS = 15_000;

interface Props {
  token: string;
  agentId: string;
  onExpired: () => void;
}

function fmtRelative(iso: string | null): string {
  if (!iso) return '—';
  const secs = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
  if (secs < 60) return `${secs}s ago`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m ago`;
  if (secs < 86400) return `${Math.floor(secs / 3600)}h ago`;
  return `${Math.floor(secs / 86400)}d ago`;
}

function fmtLatency(ms: number | null): string {
  if (ms == null) return '—';
  if (ms < 1000) return `${ms} ms`;
  return `${(ms / 1000).toFixed(2)} s`;
}

function statusLabel(e: Endpoint): { text: string; className: string } {
  if (e.last_ok == null) return { text: 'Pending', className: 'pending' };
  if (e.last_ok) return { text: 'Healthy', className: 'up' };
  return { text: 'Unhealthy', className: 'down' };
}

// fmtCertAge returns days-remaining text + color class for the SSL tile.
// Returns null on plain-http endpoints (no cert observed).
function fmtCertAge(notAfter: string | null | undefined): { text: string; className: string } | null {
  if (!notAfter) return null;
  const ms = new Date(notAfter).getTime() - Date.now();
  if (Number.isNaN(ms)) return null;
  const days = Math.floor(ms / 86_400_000);
  let className = 'cert-ok';
  if (days < 0) className = 'cert-expired';
  else if (days < 14) className = 'cert-critical';
  else if (days < 30) className = 'cert-warn';
  let text: string;
  if (days < 0) text = `expired ${-days}d ago`;
  else if (days === 0) text = 'expires today';
  else if (days < 90) text = `${days}d left`;
  else if (days < 365) text = `${Math.round(days / 30)}mo left`;
  else text = `${Math.round(days / 365)}y left`;
  return { text, className };
}

export default function EndpointsPanel({ token, agentId, onExpired }: Props) {
  const [endpoints, setEndpoints] = useState<Endpoint[]>([]);
  const [history, setHistory] = useState<Record<number, EndpointProbe[]>>({});
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [showAdd, setShowAdd] = useState(false);
  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [adding, setAdding] = useState(false);
  const [addErr, setAddErr] = useState<string | null>(null);

  // Edit modal state. `editing` is the endpoint being edited; null = closed.
  const [editing, setEditing] = useState<Endpoint | null>(null);
  const [editName, setEditName] = useState('');
  const [editUrl, setEditUrl] = useState('');
  const [savingEdit, setSavingEdit] = useState(false);
  const [editErr, setEditErr] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const list = await listEndpoints(token, agentId);
      setEndpoints(list ?? []);
      // Fetch recent probes per endpoint in parallel for the heartbeat strip.
      const probes = await Promise.all(
        (list ?? []).map(e =>
          getEndpointProbes(token, e.id, 60).catch(() => [] as EndpointProbe[]),
        ),
      );
      const map: Record<number, EndpointProbe[]> = {};
      (list ?? []).forEach((e, i) => { map[e.id] = probes[i]; });
      setHistory(map);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') { onExpired(); return; }
      setError(err instanceof Error ? err.message : 'load failed');
    } finally {
      setLoading(false);
    }
  }, [token, agentId, onExpired]);

  useEffect(() => {
    load();
    const id = setInterval(load, REFRESH_MS);
    return () => clearInterval(id);
  }, [load]);

  const handleAdd = async (e: React.FormEvent) => {
    e.preventDefault();
    setAdding(true);
    setAddErr(null);
    try {
      const created = await createEndpoint(token, agentId, name.trim(), url.trim());
      setEndpoints(prev => [...prev, { ...created, last_probed_at: null, last_status_code: null, last_latency_ms: null, last_ok: null }]);
      setName(''); setUrl(''); setShowAdd(false);
    } catch (err) {
      setAddErr(err instanceof Error ? err.message : 'add failed');
    } finally {
      setAdding(false);
    }
  };

  const openEdit = (e: Endpoint) => {
    setEditing(e);
    setEditName(e.name);
    setEditUrl(e.url);
    setEditErr(null);
  };

  const closeEdit = () => {
    if (savingEdit) return;
    setEditing(null);
  };

  const handleEdit = async (ev: React.FormEvent) => {
    ev.preventDefault();
    if (!editing) return;
    setSavingEdit(true);
    setEditErr(null);
    try {
      const updated = await updateEndpoint(token, editing.id, editName.trim(), editUrl.trim());
      setEndpoints(prev => prev.map(x => x.id === updated.id ? { ...x, ...updated } : x));
      setEditing(null);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') { onExpired(); return; }
      setEditErr(err instanceof Error ? err.message : 'save failed');
    } finally {
      setSavingEdit(false);
    }
  };

  const handleDelete = async (e: Endpoint) => {
    if (!confirm(`Delete endpoint "${e.name}"?`)) return;
    try {
      await deleteEndpoint(token, e.id);
      setEndpoints(prev => prev.filter(x => x.id !== e.id));
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') onExpired();
      else setError(err instanceof Error ? err.message : 'delete failed');
    }
  };

  return (
    <main className="dash-main">
      <div className="detail-section">
        <div className="chart-header">
          <div>
            <span className="detail-section-title">Endpoint monitoring</span>
            <div className="detail-subtitle" style={{ marginTop: 6 }}>
              GET probe per endpoint at the configured interval. 2xx and 3xx
              responses count as healthy. Latency is measured server-side.
              Set the polling cadence in <code>Settings → Endpoint monitoring</code>.
            </div>
          </div>
          <button type="button" className="btn-primary" onClick={() => setShowAdd(true)}>
            + Add endpoint
          </button>
        </div>

        {error && <div className="login-error">{error}</div>}

        {loading ? (
          <div className="dash-loading">Loading endpoints…</div>
        ) : endpoints.length === 0 ? (
          <div className="dash-empty">
            No endpoints yet. Click <strong>Add endpoint</strong> to start monitoring a URL.
          </div>
        ) : (
          <div className="endpoint-list">
            {endpoints.map(e => {
              const status = statusLabel(e);
              const probes = history[e.id] ?? [];
              return (
                <div key={e.id} className="endpoint-card">
                  <div className="endpoint-head">
                    <div className="endpoint-titles">
                      <span className="endpoint-name">{e.name}</span>
                      <a href={e.url} target="_blank" rel="noreferrer" className="endpoint-url">
                        {e.url}
                      </a>
                    </div>
                    <div className="endpoint-meta">
                      <span className={`ctr-status ${status.className}`}>{status.text}</span>
                      <button
                        type="button"
                        className="btn-secondary"
                        onClick={() => openEdit(e)}
                        aria-label={`Edit endpoint ${e.name}`}
                      >
                        Edit
                      </button>
                      <button
                        type="button"
                        className="btn-secondary"
                        onClick={() => handleDelete(e)}
                        aria-label={`Delete endpoint ${e.name}`}
                      >
                        Delete
                      </button>
                    </div>
                  </div>

                  <div className="endpoint-stats">
                    <div className="endpoint-stat">
                      <span className="endpoint-stat-label">Latency</span>
                      <span className="endpoint-stat-val mono num">{fmtLatency(e.last_latency_ms)}</span>
                    </div>
                    <div className="endpoint-stat">
                      <span className="endpoint-stat-label">Status</span>
                      <span className="endpoint-stat-val mono num">{e.last_status_code ?? '—'}</span>
                    </div>
                    <div className="endpoint-stat">
                      <span className="endpoint-stat-label">Last check</span>
                      <span className="endpoint-stat-val mono">{fmtRelative(e.last_probed_at)}</span>
                    </div>
                    {(() => {
                      const cert = fmtCertAge(e.last_cert_not_after);
                      if (!cert) return null;
                      return (
                        <div className="endpoint-stat">
                          <span className="endpoint-stat-label">SSL</span>
                          <span
                            className={`endpoint-stat-val mono ${cert.className}`}
                            title={e.last_cert_not_after
                              ? `expires ${new Date(e.last_cert_not_after).toLocaleString()}`
                              : ''}
                          >
                            {cert.text}
                          </span>
                        </div>
                      );
                    })()}
                  </div>

                  <div className="heartbeat" role="img" aria-label={`Last ${HEARTBEAT_BARS} probes`}>
                    {(() => {
                      const tail = probes.slice(-HEARTBEAT_BARS);
                      const pad = HEARTBEAT_BARS - tail.length;
                      return (
                        <>
                          {Array.from({ length: pad }).map((_, i) => (
                            <span key={`p${i}`} className="hb hb-empty" />
                          ))}
                          {tail.map(p => (
                            <span
                              key={p.id}
                              className={`hb ${p.ok ? 'hb-up' : 'hb-down'}`}
                              title={`${new Date(p.probed_at).toLocaleString()} · ${p.status_code || 'err'} · ${p.latency_ms} ms${p.error ? ' · ' + p.error : ''}`}
                            />
                          ))}
                        </>
                      );
                    })()}
                  </div>
                  {e.last_error && <div className="endpoint-err">{e.last_error}</div>}
                </div>
              );
            })}
          </div>
        )}
      </div>

      {editing && (
        <div className="modal-overlay" onClick={closeEdit} role="dialog" aria-modal="true" aria-labelledby="edit-endpoint-title">
          <div className="modal" onClick={ev => ev.stopPropagation()}>
            <h3 className="modal-title" id="edit-endpoint-title">Edit endpoint</h3>
            <p className="modal-text">
              Probe history is preserved across edits — it stays linked to this
              entry, not to the URL.
            </p>
            {editErr && <div className="login-error" style={{ marginBottom: 12 }}>{editErr}</div>}
            <form onSubmit={handleEdit}>
              <label className="form-label">
                Name
                <input
                  type="text"
                  className="form-input"
                  value={editName}
                  onChange={ev => setEditName(ev.target.value)}
                  maxLength={128}
                  autoFocus
                />
              </label>
              <label className="form-label" style={{ marginTop: 12 }}>
                URL
                <input
                  type="url"
                  className="form-input"
                  value={editUrl}
                  onChange={ev => setEditUrl(ev.target.value)}
                  required
                />
              </label>
              <div className="modal-actions">
                <button type="button" className="btn-secondary" onClick={closeEdit} disabled={savingEdit}>Cancel</button>
                <button type="submit" className="btn-primary" disabled={savingEdit}>
                  {savingEdit ? 'Saving…' : 'Save'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {showAdd && (
        <div className="modal-overlay" onClick={() => !adding && setShowAdd(false)}>
          <div className="modal" onClick={ev => ev.stopPropagation()}>
            <h3 className="modal-title">Add endpoint</h3>
            {addErr && <div className="login-error" style={{ marginBottom: 12 }}>{addErr}</div>}
            <form onSubmit={handleAdd}>
              <label className="form-label">
                Name (optional)
                <input
                  type="text"
                  className="form-input"
                  value={name}
                  onChange={ev => setName(ev.target.value)}
                  placeholder="e.g. google homepage"
                  maxLength={128}
                />
              </label>
              <label className="form-label" style={{ marginTop: 12 }}>
                URL
                <input
                  type="url"
                  className="form-input"
                  value={url}
                  onChange={ev => setUrl(ev.target.value)}
                  placeholder="https://example.com/health"
                  required
                  autoFocus
                />
              </label>
              <div className="modal-actions">
                <button type="button" className="btn-secondary" onClick={() => setShowAdd(false)} disabled={adding}>Cancel</button>
                <button type="submit" className="btn-primary" disabled={adding}>
                  {adding ? 'Adding…' : 'Add'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </main>
  );
}
