import { useEffect, useMemo, useRef, useState, useCallback } from 'react';
import {
  getAlertRule,
  saveAlertRule,
  listWebhooks,
  listEndpoints,
  type AlertRule,
  type Endpoint,
  type Webhook,
} from '../api/client';

interface Props {
  token: string;
  agentId: string;
  onExpired: () => void;
  onNavigateSettings: () => void;
}

type MetricKey = 'cpu' | 'mem' | 'disk';

const METRICS: { key: MetricKey; label: string; enabledField: keyof AlertRule; thresholdField: keyof AlertRule; defaultThreshold: number }[] = [
  { key: 'cpu',  label: 'CPU',    enabledField: 'cpu_enabled',  thresholdField: 'cpu_threshold',  defaultThreshold: 80 },
  { key: 'mem',  label: 'Memory', enabledField: 'mem_enabled',  thresholdField: 'mem_threshold',  defaultThreshold: 85 },
  { key: 'disk', label: 'Disk',   enabledField: 'disk_enabled', thresholdField: 'disk_threshold', defaultThreshold: 90 },
];

export default function AlertRuleForm({ token, agentId, onExpired, onNavigateSettings }: Props) {
  const [rule, setRule] = useState<AlertRule | null>(null);
  // Last server-confirmed copy; used to detect a dirty form for the Save button.
  const [pristine, setPristine] = useState<AlertRule | null>(null);
  const [webhooks, setWebhooks] = useState<Webhook[]>([]);
  const [endpoints, setEndpoints] = useState<Endpoint[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const savedTimer = useRef<number | null>(null);

  const load = useCallback(async () => {
    try {
      const [r, wh, eps] = await Promise.all([
        getAlertRule(token, agentId),
        listWebhooks(token),
        listEndpoints(token, agentId).catch(() => [] as Endpoint[]),
      ]);
      setRule(r);
      setPristine(r);
      setWebhooks(wh ?? []);
      setEndpoints(eps ?? []);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') {
        onExpired();
        return;
      }
      setError(err instanceof Error ? err.message : 'load failed');
    } finally {
      setLoading(false);
    }
  }, [token, agentId, onExpired]);

  useEffect(() => { load(); }, [load]);

  useEffect(() => () => {
    if (savedTimer.current != null) window.clearTimeout(savedTimer.current);
  }, []);

  const isDirty = useMemo(() => {
    if (!rule || !pristine) return false;
    return JSON.stringify(rule) !== JSON.stringify(pristine);
  }, [rule, pristine]);

  const update = useCallback(<K extends keyof AlertRule>(k: K, v: AlertRule[K]) => {
    setRule(prev => prev ? { ...prev, [k]: v } : prev);
    setSaved(false);
  }, []);

  const save = useCallback(async () => {
    if (!rule) return;
    setSaving(true);
    setError(null);
    try {
      const next = await saveAlertRule(token, rule);
      setRule(next);
      setPristine(next);
      setSaved(true);
      if (savedTimer.current != null) window.clearTimeout(savedTimer.current);
      savedTimer.current = window.setTimeout(() => setSaved(false), 2500);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') {
        onExpired();
        return;
      }
      setError(err instanceof Error ? err.message : 'save failed');
    } finally {
      setSaving(false);
    }
  }, [rule, token, onExpired]);

  if (loading) return <div className="dash-loading" style={{ padding: 24 }}>Loading rules…</div>;
  if (!rule) return null;

  const noWebhooks = webhooks.length === 0;

  return (
    <div className="alert-rule-form">
      <div className="alert-rule-section">
        <div className="alert-rule-section-head">
          <div className="alert-rule-section-title">System alerts</div>
          <span className="alert-rule-kind-badge">Host metrics</span>
        </div>
        <p className="settings-hint" style={{ marginBottom: 12 }}>
          Monitors the host's CPU, memory, and disk usage. A rule fires when
          the value exceeds the threshold continuously for at least 1 minute.
          Container-level, uptime, and reachability alerts are configured
          separately.
        </p>

        <div className="metric-rule-list">
          {/* Agent reachability — boolean toggle. Sustain window comes from
              the global alert timing setting. */}
          <label className="metric-rule-row metric-rule-row-single">
            <span className="metric-rule-checkbox">
              <input
                type="checkbox"
                checked={rule.agent_down_enabled}
                onChange={e => update('agent_down_enabled', e.target.checked as never)}
                aria-label="Alert when this agent stops reporting"
              />
              <span className="metric-rule-label">Agent down</span>
            </span>
            <span className="metric-rule-threshold metric-rule-bool">
              <span className="metric-rule-unit">no heartbeat past sustain window</span>
            </span>
          </label>

          {METRICS.map(m => {
            const enabled = rule[m.enabledField] as boolean;
            const threshold = rule[m.thresholdField] as number;
            return (
              <div key={m.key} className="metric-rule-row">
                <label className="metric-rule-checkbox">
                  <input
                    type="checkbox"
                    checked={enabled}
                    onChange={e => update(m.enabledField, e.target.checked as never)}
                    aria-label={`Monitor ${m.label}`}
                  />
                  <span className="metric-rule-label">{m.label}</span>
                </label>
                <div className="metric-rule-threshold">
                  <span className="metric-rule-op">&gt;</span>
                  <input
                    type="number"
                    min={1}
                    max={100}
                    className="form-input"
                    value={threshold}
                    disabled={!enabled}
                    onChange={e => {
                      const n = parseInt(e.target.value, 10);
                      if (Number.isFinite(n)) update(m.thresholdField, n as never);
                    }}
                    aria-label={`${m.label} threshold percent`}
                  />
                  <span className="metric-rule-unit">%</span>
                </div>
              </div>
            );
          })}
        </div>
      </div>

      <div className="alert-rule-section">
        <div className="alert-rule-section-head">
          <div className="alert-rule-section-title">Container alerts</div>
          <span className="alert-rule-kind-badge">Per container</span>
        </div>
        <p className="settings-hint" style={{ marginBottom: 12 }}>
          Applies to every container reported by this agent. Each rule fires
          independently per container; the breach must hold for the configured
          sustain window before notifying.
        </p>

        <div className="metric-rule-list">
          {/* Not running */}
          <div className="metric-rule-row">
            <label className="metric-rule-checkbox">
              <input
                type="checkbox"
                checked={rule.ctr_down_enabled}
                onChange={e => update('ctr_down_enabled', e.target.checked)}
                aria-label="Alert when a container is not running"
              />
              <span className="metric-rule-label">Container not running</span>
            </label>
            <span className="metric-rule-threshold metric-rule-bool">
              <span className="metric-rule-unit">fires while down</span>
            </span>
          </div>

          {/* CPU mCore */}
          <div className="metric-rule-row">
            <label className="metric-rule-checkbox">
              <input
                type="checkbox"
                checked={rule.ctr_cpu_enabled}
                onChange={e => update('ctr_cpu_enabled', e.target.checked)}
                aria-label="Monitor container CPU"
              />
              <span className="metric-rule-label">Container CPU</span>
            </label>
            <div className="metric-rule-threshold">
              <span className="metric-rule-op">&gt;</span>
              <input
                type="number"
                min={1}
                max={64000}
                step={50}
                className="form-input"
                value={rule.ctr_cpu_threshold_mcore}
                disabled={!rule.ctr_cpu_enabled}
                onChange={e => {
                  const n = parseInt(e.target.value, 10);
                  if (Number.isFinite(n)) update('ctr_cpu_threshold_mcore', n);
                }}
                aria-label="Container CPU threshold in mCore"
              />
              <span className="metric-rule-unit">mCore</span>
            </div>
          </div>

          {/* Memory % of limit */}
          <div className="metric-rule-row">
            <label className="metric-rule-checkbox">
              <input
                type="checkbox"
                checked={rule.ctr_mem_enabled}
                onChange={e => update('ctr_mem_enabled', e.target.checked)}
                aria-label="Monitor container memory"
              />
              <span className="metric-rule-label">Container memory</span>
            </label>
            <div className="metric-rule-threshold">
              <span className="metric-rule-op">&gt;</span>
              <input
                type="number"
                min={1}
                max={100}
                className="form-input"
                value={rule.ctr_mem_threshold}
                disabled={!rule.ctr_mem_enabled}
                onChange={e => {
                  const n = parseInt(e.target.value, 10);
                  if (Number.isFinite(n)) update('ctr_mem_threshold', n);
                }}
                aria-label="Container memory threshold percent"
              />
              <span className="metric-rule-unit">%</span>
            </div>
          </div>
        </div>
      </div>

      <div className="alert-rule-section">
        <div className="alert-rule-section-head">
          <div className="alert-rule-section-title">Endpoint alerts</div>
          <span className="alert-rule-kind-badge">All endpoints</span>
        </div>
        <p className="settings-hint" style={{ marginBottom: 12 }}>
          Master switch for endpoint down/up alerts. When on, every endpoint
          configured for this agent fires a notification once its probe stops
          returning a 2xx/3xx response, and again when it recovers. The first
          probe after enabling is silent so existing state isn't re-announced.
        </p>

        <label className="metric-rule-row metric-rule-row-single">
          <span className="metric-rule-checkbox">
            <input
              type="checkbox"
              checked={rule.endpoint_down_enabled}
              onChange={e => update('endpoint_down_enabled', e.target.checked as never)}
              aria-label="Enable endpoint down/up alerts for this agent"
            />
            <span className="metric-rule-label">Endpoint down / up</span>
          </span>
          <span className="metric-rule-threshold metric-rule-bool">
            <span className="metric-rule-unit">
              {endpoints.length === 0
                ? 'no endpoints configured yet'
                : `applies to ${endpoints.length} endpoint${endpoints.length === 1 ? '' : 's'}`}
            </span>
          </span>
        </label>

        <label className="metric-rule-row metric-rule-row-single">
          <span className="metric-rule-checkbox">
            <input
              type="checkbox"
              checked={rule.ssl_alert_enabled}
              onChange={e => update('ssl_alert_enabled', e.target.checked as never)}
              aria-label="Enable SSL expiry alerts for this agent"
            />
            <span className="metric-rule-label">SSL expiry warning</span>
          </span>
          <span className="metric-rule-threshold metric-rule-bool">
            <span className="metric-rule-unit">fires when leaf cert ≤ 14 days from expiry</span>
          </span>
        </label>
      </div>

      <div className="alert-rule-section">
        <div className="alert-rule-section-title">Webhook</div>
        <p className="settings-hint" style={{ marginBottom: 12 }}>
          The named webhook that receives the notification payload when a
          threshold is breached.
        </p>

        {noWebhooks ? (
          <div className="alerts-placeholder" style={{ marginTop: 0 }}>
            <div className="alerts-placeholder-title">No webhooks configured</div>
            <p className="alerts-placeholder-hint" style={{ marginBottom: 12 }}>
              Add a webhook in Settings first, then return here to bind it to this rule.
            </p>
            <button type="button" className="btn-primary" onClick={onNavigateSettings}>
              Configure webhook →
            </button>
          </div>
        ) : (
          <select
            className="form-input"
            value={rule.webhook_id == null ? '' : String(rule.webhook_id)}
            onChange={e => {
              const v = e.target.value;
              update('webhook_id', v === '' ? null : parseInt(v, 10));
            }}
            aria-label="Webhook"
          >
            <option value="">— Select a webhook —</option>
            {webhooks.map(w => (
              <option key={w.id} value={w.id}>{w.name}</option>
            ))}
          </select>
        )}
      </div>

      {error && <div className="login-error">{error}</div>}

      <div className="alert-rule-actions">
        <div className="alert-rule-save-hint" aria-live="polite">
          {saved
            ? <span className="alert-rule-saved">Saved</span>
            : isDirty ? <span>Unsaved changes</span> : null}
        </div>
        <button
          type="button"
          className="btn-primary"
          onClick={save}
          disabled={saving || !isDirty}
          title={!isDirty ? 'No changes to save' : undefined}
        >
          {saving ? 'Saving…' : 'Save rule'}
        </button>
      </div>
    </div>
  );
}
