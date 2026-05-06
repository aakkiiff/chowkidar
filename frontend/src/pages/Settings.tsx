import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import { useOutletContext } from 'react-router-dom';
import {
  listWebhooks,
  createWebhook,
  deleteWebhook,
  getAlertSettings,
  saveAlertSettings,
  getEndpointSettings,
  saveEndpointSettings,
  listUsers,
  createUser,
  deleteUser,
  setUserAgents,
  changeOwnPassword,
  listAgents,
  type AlertSettings,
  type AppUser,
  type Agent,
  type EndpointSettings,
  type Role,
  type Webhook,
  type WebhookType,
} from '../api/client';
import ThemeToggle from '../components/ThemeToggle';
import type { AuthCtx } from './Protected';

const WEBHOOK_TYPES: { value: WebhookType; label: string }[] = [
  { value: 'discord', label: 'Discord' },
];

export default function Settings() {
  const { token, username, role, onLogout: onExpired } = useOutletContext<AuthCtx>();
  const isAdmin = role === 'admin';
  const [webhooks, setWebhooks] = useState<Webhook[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [name, setName] = useState('');
  const [url, setUrl] = useState('');
  const [wtype, setWtype] = useState<WebhookType>('discord');
  const [saving, setSaving] = useState(false);

  // Global alert timing.
  const [alertCfg, setAlertCfg] = useState<AlertSettings | null>(null);
  const [alertCfgPristine, setAlertCfgPristine] = useState<AlertSettings | null>(null);
  const [alertCfgSaving, setAlertCfgSaving] = useState(false);
  const [alertCfgSaved, setAlertCfgSaved] = useState(false);
  const [alertCfgErr, setAlertCfgErr] = useState<string | null>(null);
  const alertSavedTimer = useRef<number | null>(null);

  // User management (admin only).
  const [users, setUsers] = useState<AppUser[]>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [usersLoading, setUsersLoading] = useState(true);
  const [newUserName, setNewUserName] = useState('');
  const [newUserPass, setNewUserPass] = useState('');
  const [newUserRole, setNewUserRole] = useState<Role>('developer');
  const [newUserAgentIds, setNewUserAgentIds] = useState<string[]>([]);
  const [creatingUser, setCreatingUser] = useState(false);
  const [userErr, setUserErr] = useState<string | null>(null);

  // Edit-access modal for developer permissions.
  const [editingUser, setEditingUser] = useState<AppUser | null>(null);
  const [editingAgentIds, setEditingAgentIds] = useState<string[]>([]);
  const [editingSaving, setEditingSaving] = useState(false);
  const [editingSaved, setEditingSaved] = useState(false);
  const editSavedTimer = useRef<number | null>(null);

  // Change own password.
  const [pwCurrent, setPwCurrent] = useState('');
  const [pwNew, setPwNew] = useState('');
  const [pwConfirm, setPwConfirm] = useState('');
  const [pwSaving, setPwSaving] = useState(false);
  const [pwErr, setPwErr] = useState<string | null>(null);
  const [pwSaved, setPwSaved] = useState(false);
  const pwSavedTimer = useRef<number | null>(null);

  const handleChangePassword = async (e: React.FormEvent) => {
    e.preventDefault();
    setPwErr(null);
    if (pwNew !== pwConfirm) { setPwErr('Passwords do not match'); return; }
    setPwSaving(true);
    try {
      await changeOwnPassword(token, pwCurrent, pwNew);
      setPwCurrent(''); setPwNew(''); setPwConfirm('');
      setPwSaved(true);
      if (pwSavedTimer.current != null) window.clearTimeout(pwSavedTimer.current);
      pwSavedTimer.current = window.setTimeout(() => setPwSaved(false), 2500);
    } catch (err) {
      setPwErr(err instanceof Error ? err.message : 'failed');
    } finally {
      setPwSaving(false);
    }
  };

  // Endpoint monitoring (probe interval).
  const [epCfg, setEpCfg] = useState<EndpointSettings | null>(null);
  const [epCfgPristine, setEpCfgPristine] = useState<EndpointSettings | null>(null);
  const [epCfgSaving, setEpCfgSaving] = useState(false);
  const [epCfgSaved, setEpCfgSaved] = useState(false);
  const [epCfgErr, setEpCfgErr] = useState<string | null>(null);
  const epSavedTimer = useRef<number | null>(null);

  const load = useCallback(async () => {
    if (!isAdmin) {
      setLoading(false);
      setUsersLoading(false);
      return;
    }
    try {
      const [hooks, settings, epSettings, userList, agentList] = await Promise.all([
        listWebhooks(token),
        getAlertSettings(token),
        getEndpointSettings(token),
        listUsers(token).catch(() => [] as AppUser[]),
        listAgents(token).catch(() => [] as Agent[]),
      ]);
      setWebhooks(hooks ?? []);
      setAlertCfg(settings);
      setAlertCfgPristine(settings);
      setEpCfg(epSettings);
      setEpCfgPristine(epSettings);
      setUsers(userList ?? []);
      setAgents(agentList ?? []);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') {
        onExpired();
        return;
      }
      setError(err instanceof Error ? err.message : 'load failed');
    } finally {
      setLoading(false);
      setUsersLoading(false);
    }
  }, [token, isAdmin, onExpired]);

  useEffect(() => { load(); }, [load]);

  useEffect(() => () => {
    if (alertSavedTimer.current != null) window.clearTimeout(alertSavedTimer.current);
  }, []);

  const alertCfgDirty = useMemo(() => {
    if (!alertCfg || !alertCfgPristine) return false;
    return JSON.stringify(alertCfg) !== JSON.stringify(alertCfgPristine);
  }, [alertCfg, alertCfgPristine]);

  const epCfgDirty = useMemo(() => {
    if (!epCfg || !epCfgPristine) return false;
    return JSON.stringify(epCfg) !== JSON.stringify(epCfgPristine);
  }, [epCfg, epCfgPristine]);

  const saveEpCfg = async () => {
    if (!epCfg) return;
    setEpCfgSaving(true);
    setEpCfgErr(null);
    try {
      const next = await saveEndpointSettings(token, epCfg);
      setEpCfg(next);
      setEpCfgPristine(next);
      setEpCfgSaved(true);
      if (epSavedTimer.current != null) window.clearTimeout(epSavedTimer.current);
      epSavedTimer.current = window.setTimeout(() => setEpCfgSaved(false), 2500);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') { onExpired(); return; }
      setEpCfgErr(err instanceof Error ? err.message : 'save failed');
    } finally {
      setEpCfgSaving(false);
    }
  };

  const saveAlertCfg = async () => {
    if (!alertCfg) return;
    setAlertCfgSaving(true);
    setAlertCfgErr(null);
    try {
      const next = await saveAlertSettings(token, alertCfg);
      setAlertCfg(next);
      setAlertCfgPristine(next);
      setAlertCfgSaved(true);
      if (alertSavedTimer.current != null) window.clearTimeout(alertSavedTimer.current);
      alertSavedTimer.current = window.setTimeout(() => setAlertCfgSaved(false), 2500);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') {
        onExpired();
        return;
      }
      setAlertCfgErr(err instanceof Error ? err.message : 'save failed');
    } finally {
      setAlertCfgSaving(false);
    }
  };

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSaving(true);
    try {
      const hook = await createWebhook(token, name.trim(), url.trim(), wtype);
      setWebhooks(prev => [...prev, hook].sort((a, b) => a.name.localeCompare(b.name)));
      setName('');
      setUrl('');
      setWtype('discord');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'save failed');
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (id: number, webhookName: string) => {
    if (!confirm(`Delete webhook "${webhookName}"?`)) return;
    try {
      await deleteWebhook(token, id);
      setWebhooks(prev => prev.filter(w => w.id !== id));
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') {
        onExpired();
        return;
      }
      setError(err instanceof Error ? err.message : 'delete failed');
    }
  };

  const toggleAgent = (agentId: string, list: string[], setList: (v: string[]) => void) => {
    setList(list.includes(agentId) ? list.filter(id => id !== agentId) : [...list, agentId]);
  };

  const saveEditAccess = async () => {
    if (!editingUser) return;
    setEditingSaving(true);
    try {
      await setUserAgents(token, editingUser.id, editingAgentIds);
      setUsers(prev => prev.map(u =>
        u.id === editingUser.id ? { ...u, agent_ids: editingAgentIds } : u
      ));
      setEditingSaved(true);
      if (editSavedTimer.current != null) window.clearTimeout(editSavedTimer.current);
      editSavedTimer.current = window.setTimeout(() => {
        setEditingSaved(false);
        setEditingUser(null);
      }, 900);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') { onExpired(); return; }
      setUserErr(err instanceof Error ? err.message : 'save failed');
    } finally {
      setEditingSaving(false);
    }
  };

  const agentName = (id: string) => {
    const a = agents.find(x => x.id === id);
    if (!a) return id.slice(0, 12);
    const proj = a.project_environment
      ? `${a.project_name} (${a.project_environment})`
      : a.project_name;
    return proj ? `${proj} — ${a.hostname}` : a.hostname;
  };

  return (
    <main className="dash-main">
    <div className="dash-section">
      <div className="dash-section-header">
        <h2 className="dash-section-title">Settings</h2>
      </div>

      <section className="settings-block" style={{ marginBottom: 16 }}>
        <h3 className="settings-block-title">Appearance</h3>
        <p className="settings-hint">Switch between light and dark themes.</p>
        <ThemeToggle />
      </section>


      {isAdmin && (
      <>
      <section className="settings-block" style={{ marginBottom: 16 }}>
        <h3 className="settings-block-title">Alert timing</h3>
        <p className="settings-hint">
          System-wide timing for agent alert rules. A breach must persist for
          the sustain window before a webhook fires, preventing pages from
          temporary spikes. If the breach is still unresolved, the resend
          cooldown controls how often follow-up notifications fire. Set resend
          to 0 to notify only once per incident.
        </p>

        {alertCfg && (
          <div className="alert-timing-grid">
            <label className="alert-timing-field">
              <span className="alert-timing-label">Sustain window</span>
              <div className="alert-timing-input">
                <input
                  type="number"
                  min={5}
                  max={3600}
                  className="form-input"
                  value={alertCfg.sustain_seconds}
                  onChange={e => {
                    const n = parseInt(e.target.value, 10);
                    if (Number.isFinite(n)) setAlertCfg({ ...alertCfg, sustain_seconds: n });
                  }}
                  aria-label="Sustain seconds"
                />
                <span className="alert-timing-unit">seconds</span>
              </div>
              <span className="alert-timing-hint">Breach must hold this long before firing. 5–3600.</span>
            </label>

            <label className="alert-timing-field">
              <span className="alert-timing-label">Resend cooldown</span>
              <div className="alert-timing-input">
                <input
                  type="number"
                  min={0}
                  max={86400}
                  className="form-input"
                  value={alertCfg.resend_cooldown_seconds}
                  onChange={e => {
                    const n = parseInt(e.target.value, 10);
                    if (Number.isFinite(n)) setAlertCfg({ ...alertCfg, resend_cooldown_seconds: n });
                  }}
                  aria-label="Resend cooldown seconds"
                />
                <span className="alert-timing-unit">seconds</span>
              </div>
              <span className="alert-timing-hint">0 disables resend. Else re-fire every N seconds while still breaching.</span>
            </label>
          </div>
        )}

        {alertCfgErr && <div className="login-error" style={{ marginTop: 12 }}>{alertCfgErr}</div>}

        <div className="alert-rule-actions" style={{ marginTop: 14 }}>
          <div className="alert-rule-save-hint" aria-live="polite">
            {alertCfgSaved
              ? <span className="alert-rule-saved">Saved</span>
              : alertCfgDirty ? <span>Unsaved changes</span> : null}
          </div>
          <button
            type="button"
            className="btn-primary"
            onClick={saveAlertCfg}
            disabled={alertCfgSaving || !alertCfgDirty}
            title={!alertCfgDirty ? 'No changes to save' : undefined}
          >
            {alertCfgSaving ? 'Saving…' : 'Save timing'}
          </button>
        </div>
      </section>

      <section className="settings-block" style={{ marginBottom: 16 }}>
        <h3 className="settings-block-title">Endpoint monitoring</h3>
        <p className="settings-hint">
          How often the central prober checks each configured URL. Lower values
          give finer-grained heartbeat history at the cost of more outbound
          requests. Per-endpoint URLs are configured under each agent's
          <strong> Endpoints </strong>tab.
        </p>

        {epCfgErr && <div className="login-error" style={{ marginBottom: 12 }}>{epCfgErr}</div>}

        {epCfg && (
          <div className="alert-timing-grid">
            <label className="form-label">
              Probe interval (seconds)
              <input
                type="number"
                className="form-input"
                min={10}
                max={3600}
                value={epCfg.probe_interval_seconds}
                onChange={e => {
                  const n = parseInt(e.target.value, 10);
                  if (Number.isFinite(n)) setEpCfg({ ...epCfg, probe_interval_seconds: n });
                  setEpCfgSaved(false);
                }}
              />
              <span className="settings-hint">10 – 3600. Default 60.</span>
            </label>
            <label className="form-label">
              Incident retention (days)
              <input
                type="number"
                className="form-input"
                min={1}
                max={365}
                value={epCfg.incident_retention_days}
                onChange={e => {
                  const n = parseInt(e.target.value, 10);
                  if (Number.isFinite(n)) setEpCfg({ ...epCfg, incident_retention_days: n });
                  setEpCfgSaved(false);
                }}
              />
              <span className="settings-hint">
                Outage windows used by uptime % + gantt. 1 – 365. Default 30. Ongoing outages never pruned.
              </span>
            </label>
          </div>
        )}

        <div className="alert-rule-actions">
          <div className="alert-rule-save-hint" aria-live="polite">
            {epCfgSaved
              ? <span className="alert-rule-saved">Saved</span>
              : epCfgDirty ? <span>Unsaved changes</span> : null}
          </div>
          <button
            type="button"
            className="btn-primary"
            onClick={saveEpCfg}
            disabled={epCfgSaving || !epCfgDirty}
            title={!epCfgDirty ? 'No changes to save' : undefined}
          >
            {epCfgSaving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </section>

      <section className="settings-block">
        <h3 className="settings-block-title">Webhooks</h3>
        <p className="settings-hint">
          Named HTTP endpoints. Reference a webhook by its name when configuring
          alerts on an agent.
        </p>

        <form className="webhook-form" onSubmit={handleCreate}>
          <select
            className="form-input"
            value={wtype}
            onChange={e => setWtype(e.target.value as WebhookType)}
            aria-label="Webhook type"
          >
            {WEBHOOK_TYPES.map(t => (
              <option key={t.value} value={t.value}>{t.label}</option>
            ))}
          </select>
          <input
            type="text"
            className="form-input"
            placeholder="Name (e.g. ops-alerts)"
            value={name}
            onChange={e => setName(e.target.value)}
            maxLength={64}
            required
          />
          <input
            type="url"
            className="form-input"
            placeholder="https://discord.com/api/webhooks/…"
            value={url}
            onChange={e => setUrl(e.target.value)}
            required
          />
          <button type="submit" className="btn-primary" disabled={saving}>
            {saving ? 'Adding…' : 'Add webhook'}
          </button>
        </form>

        {error && <div className="login-error">{error}</div>}

        {loading ? (
          <div className="dash-loading">Loading…</div>
        ) : webhooks.length === 0 ? (
          <div className="dash-empty">No webhooks yet. Add one above.</div>
        ) : (
          <div className="table-wrap">
            <table className="container-table">
              <thead>
                <tr>
                  <th scope="col">Type</th>
                  <th scope="col">Name</th>
                  <th scope="col">URL</th>
                  <th scope="col" />
                </tr>
              </thead>
              <tbody>
                {webhooks.map(w => (
                  <tr key={w.id}>
                    <td>
                      <span className="webhook-type-badge">{w.type}</span>
                    </td>
                    <td className="td-name">{w.name}</td>
                    <td className="td-muted webhook-url">{w.url}</td>
                    <td style={{ textAlign: 'right' }}>
                      <button
                        type="button"
                        className="btn-secondary"
                        onClick={() => handleDelete(w.id, w.name)}
                        aria-label={`Delete webhook ${w.name}`}
                      >
                        Delete
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section className="settings-block" style={{ marginTop: 16 }}>
        <h3 className="settings-block-title">Users</h3>
        <p className="settings-hint">
          Admins have full access to every page. Developers can only browse the
          agents they are assigned to — they can't edit alerts, endpoints,
          webhooks, or other users.
        </p>

        <form
          className="user-form"
          onSubmit={async e => {
            e.preventDefault();
            setUserErr(null);
            setCreatingUser(true);
            try {
              const ids = newUserRole === 'developer' ? newUserAgentIds : undefined;
              const u = await createUser(token, newUserName.trim(), newUserPass, newUserRole, ids);
              setUsers(prev => [...prev, u]);
              setNewUserName(''); setNewUserPass(''); setNewUserRole('developer'); setNewUserAgentIds([]);
            } catch (err) {
              setUserErr(err instanceof Error ? err.message : 'create failed');
            } finally {
              setCreatingUser(false);
            }
          }}
        >
          <div className="user-form-row">
            <input
              type="text"
              className="form-input"
              placeholder="Username"
              value={newUserName}
              onChange={e => setNewUserName(e.target.value)}
              maxLength={64}
              required
            />
            <input
              type="password"
              className="form-input"
              placeholder="Password (≥6 chars)"
              value={newUserPass}
              onChange={e => setNewUserPass(e.target.value)}
              minLength={6}
              required
            />
            <select
              className="form-input"
              value={newUserRole}
              onChange={e => {
                setNewUserRole(e.target.value as Role);
                if (e.target.value === 'admin') setNewUserAgentIds([]);
              }}
              aria-label="Role"
            >
              <option value="developer">Developer</option>
              <option value="admin">Admin</option>
            </select>
            <button type="submit" className="btn-primary" disabled={creatingUser}>
              {creatingUser ? 'Creating…' : 'Add user'}
            </button>
          </div>

          {newUserRole === 'developer' && (
            <div className="agent-access-picker">
              <div className="agent-access-picker-title">
                Agent access
                {newUserAgentIds.length > 0 && ` · ${newUserAgentIds.length} selected`}
              </div>
              {agents.length === 0 ? (
                <span className="settings-hint" style={{ marginBottom: 0 }}>No agents registered yet — you can assign access later.</span>
              ) : (
                <div className="agent-access-picker-grid">
                  {agents.map(a => {
                    const proj = a.project_environment
                      ? `${a.project_name} (${a.project_environment})`
                      : a.project_name;
                    const label = proj ? `${proj} — ${a.hostname}` : a.hostname;
                    return (
                      <label key={a.id} className={`agent-chip${newUserAgentIds.includes(a.id) ? ' selected' : ''}`}>
                        <input
                          type="checkbox"
                          checked={newUserAgentIds.includes(a.id)}
                          onChange={() => toggleAgent(a.id, newUserAgentIds, setNewUserAgentIds)}
                        />
                        {label}
                      </label>
                    );
                  })}
                </div>
              )}
            </div>
          )}
        </form>

        {userErr && <div className="login-error">{userErr}</div>}

        {usersLoading ? (
          <div className="dash-loading">Loading…</div>
        ) : users.length === 0 ? (
          <div className="dash-empty">No users yet.</div>
        ) : (
          <div className="table-wrap">
            <table className="container-table">
              <thead>
                <tr>
                  <th scope="col">Username</th>
                  <th scope="col">Role</th>
                  <th scope="col">Agents</th>
                  <th scope="col">Created</th>
                  <th scope="col" />
                </tr>
              </thead>
              <tbody>
                {users.map(u => {
                  const isSelf = u.username === username;
                  return (
                    <tr key={u.id}>
                      <td className="td-name">{u.username}{isSelf && ' (you)'}</td>
                      <td>
                        <span className="webhook-type-badge">{u.role}</span>
                      </td>
                      <td style={{ maxWidth: 240 }}>
                        {u.role === 'developer'
                          ? (u.agent_ids?.length ?? 0) > 0
                            ? <div className="agent-tag-list">
                                {u.agent_ids!.map(id => (
                                  <span key={id} className="agent-tag">{agentName(id)}</span>
                                ))}
                              </div>
                            : <span className="td-muted" style={{ opacity: 0.5 }}>No access</span>
                          : <span className="td-muted">—</span>
                        }
                      </td>
                      <td className="td-muted mono">{new Date(u.created_at).toLocaleDateString()}</td>
                      <td style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>
                        {u.role === 'developer' && !isSelf && (
                          <button
                            type="button"
                            className="btn-secondary"
                            style={{ marginRight: 6 }}
                            onClick={() => {
                              setEditingUser(u);
                              setEditingAgentIds(u.agent_ids ?? []);
                            }}
                          >
                            Edit access
                          </button>
                        )}
                        <button
                          type="button"
                          className="btn-secondary"
                          disabled={isSelf}
                          title={isSelf ? "Can't delete your own account" : undefined}
                          onClick={async () => {
                            if (!confirm(`Delete user "${u.username}"?`)) return;
                            try {
                              await deleteUser(token, u.id);
                              setUsers(prev => prev.filter(x => x.id !== u.id));
                            } catch (err) {
                              if (err instanceof Error && err.message === 'Session expired') onExpired();
                              else setUserErr(err instanceof Error ? err.message : 'delete failed');
                            }
                          }}
                          aria-label={`Delete user ${u.username}`}
                        >
                          Delete
                        </button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {/* Edit agent access modal */}
      {editingUser && (
        <div className="modal-overlay" onClick={() => !editingSaving && setEditingUser(null)}>
          <div className="modal" style={{ maxWidth: 480 }} onClick={e => e.stopPropagation()}>
            <div className="modal-title">Agent access — {editingUser.username}</div>
            <p className="modal-text">Select which agents this developer can view.</p>

            {agents.length === 0 ? (
              <p className="modal-text">No agents registered yet.</p>
            ) : (
              <div className="agent-access-picker" style={{ marginBottom: 4 }}>
                <div className="agent-access-picker-title">
                  Agents
                  {editingAgentIds.length > 0 && ` · ${editingAgentIds.length} selected`}
                </div>
                <div className="agent-access-picker-grid">
                  {agents.map(a => {
                    const proj = a.project_environment
                      ? `${a.project_name} (${a.project_environment})`
                      : a.project_name;
                    const label = proj ? `${proj} — ${a.hostname}` : a.hostname;
                    return (
                      <label key={a.id} className={`agent-chip${editingAgentIds.includes(a.id) ? ' selected' : ''}`}>
                        <input
                          type="checkbox"
                          checked={editingAgentIds.includes(a.id)}
                          onChange={() => toggleAgent(a.id, editingAgentIds, setEditingAgentIds)}
                        />
                        {label}
                      </label>
                    );
                  })}
                </div>
              </div>
            )}

            <div className="modal-actions">
              <button type="button" className="btn-secondary" onClick={() => setEditingUser(null)} disabled={editingSaving}>
                Cancel
              </button>
              <button
                type="button"
                className="btn-primary"
                disabled={editingSaving}
                onClick={saveEditAccess}
              >
                {editingSaving ? 'Saving…' : editingSaved ? 'Saved' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      )}
      </>
      )}
      <section className="settings-block" style={{ marginTop: 16 }}>
        <details>
          <summary style={{ cursor: 'pointer', fontSize: 14, fontWeight: 600, color: 'var(--text-strong)', userSelect: 'none' }}>
            Change password
          </summary>
          <p className="settings-hint" style={{ marginTop: 8 }}>
            Signed in as <strong>{username}</strong>. Enter your current password to set a new one.
          </p>
          <form onSubmit={handleChangePassword} style={{ display: 'flex', flexDirection: 'column', gap: 10, maxWidth: 360 }}>
            <label className="form-label">
              Current password
              <input
                type="password"
                className="form-input"
                value={pwCurrent}
                onChange={e => setPwCurrent(e.target.value)}
                required
                autoComplete="current-password"
              />
            </label>
            <label className="form-label">
              New password
              <input
                type="password"
                className="form-input"
                value={pwNew}
                onChange={e => setPwNew(e.target.value)}
                minLength={6}
                required
                autoComplete="new-password"
              />
            </label>
            <label className="form-label">
              Confirm new password
              <input
                type="password"
                className="form-input"
                value={pwConfirm}
                onChange={e => setPwConfirm(e.target.value)}
                minLength={6}
                required
                autoComplete="new-password"
              />
            </label>
            {pwErr && <div className="login-error">{pwErr}</div>}
            <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
              <button type="submit" className="btn-primary" disabled={pwSaving}>
                {pwSaving ? 'Saving…' : 'Change password'}
              </button>
              {pwSaved && <span className="alert-rule-saved">Password updated</span>}
            </div>
          </form>
        </details>
      </section>
    </div>
    </main>
  );
}
