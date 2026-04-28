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
  type AlertSettings,
  type AppUser,
  type EndpointSettings,
  type Role,
  type Webhook,
  type WebhookType,
} from '../api/client';
import ThemeToggle from '../components/ThemeToggle';
import type { AuthCtx } from './Protected';

// Keep this list in sync with the server's supportedWebhookTypes map +
// alert.formatters. Adding a provider is a 3-file change.
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
  const [type, setType] = useState<WebhookType>('discord');
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
  const [usersLoading, setUsersLoading] = useState(true);
  const [newUserName, setNewUserName] = useState('');
  const [newUserPass, setNewUserPass] = useState('');
  const [newUserRole, setNewUserRole] = useState<Role>('developer');
  const [creatingUser, setCreatingUser] = useState(false);
  const [userErr, setUserErr] = useState<string | null>(null);

  // Endpoint monitoring (probe interval).
  const [epCfg, setEpCfg] = useState<EndpointSettings | null>(null);
  const [epCfgPristine, setEpCfgPristine] = useState<EndpointSettings | null>(null);
  const [epCfgSaving, setEpCfgSaving] = useState(false);
  const [epCfgSaved, setEpCfgSaved] = useState(false);
  const [epCfgErr, setEpCfgErr] = useState<string | null>(null);
  const epSavedTimer = useRef<number | null>(null);

  const load = useCallback(async () => {
    if (!isAdmin) {
      // Developers don't have access to admin config endpoints — skip
      // fetching them so the page doesn't 403-spam the console.
      setLoading(false);
      setUsersLoading(false);
      return;
    }
    try {
      const [hooks, settings, epSettings, userList] = await Promise.all([
        listWebhooks(token),
        getAlertSettings(token),
        getEndpointSettings(token),
        listUsers(token).catch(() => [] as AppUser[]),
      ]);
      setWebhooks(hooks ?? []);
      setAlertCfg(settings);
      setAlertCfgPristine(settings);
      setEpCfg(epSettings);
      setEpCfgPristine(epSettings);
      setUsers(userList ?? []);
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
      const hook = await createWebhook(token, name.trim(), url.trim(), type);
      setWebhooks(prev => [...prev, hook].sort((a, b) => a.name.localeCompare(b.name)));
      setName('');
      setUrl('');
      setType('discord');
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

      {!isAdmin && (
        <section className="settings-block" style={{ marginBottom: 16 }}>
          <h3 className="settings-block-title">Account</h3>
          <p className="settings-hint">
            Signed in as <strong>{username}</strong> · role <code>{role}</code>.
            Other settings (webhooks, alert timing, endpoint monitoring, users)
            are restricted to administrators.
          </p>
        </section>
      )}

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
            value={type}
            onChange={e => setType(e.target.value as WebhookType)}
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
          agents list and an agent's overview tab — they can't edit alerts,
          endpoints, webhooks, or other users.
        </p>

        <form
          className="webhook-form"
          onSubmit={async e => {
            e.preventDefault();
            setUserErr(null);
            setCreatingUser(true);
            try {
              const u = await createUser(token, newUserName.trim(), newUserPass, newUserRole);
              setUsers(prev => [...prev, u]);
              setNewUserName(''); setNewUserPass(''); setNewUserRole('developer');
            } catch (err) {
              setUserErr(err instanceof Error ? err.message : 'create failed');
            } finally {
              setCreatingUser(false);
            }
          }}
        >
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
            onChange={e => setNewUserRole(e.target.value as Role)}
            aria-label="Role"
          >
            <option value="developer">Developer</option>
            <option value="admin">Admin</option>
          </select>
          <button type="submit" className="btn-primary" disabled={creatingUser}>
            {creatingUser ? 'Creating…' : 'Add user'}
          </button>
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
                      <td className="td-muted mono">{new Date(u.created_at).toLocaleDateString()}</td>
                      <td style={{ textAlign: 'right' }}>
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
      </>
      )}
    </div>
    </main>
  );
}
