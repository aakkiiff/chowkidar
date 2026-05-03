import { useState, useEffect, useCallback, useMemo, type KeyboardEvent } from 'react';
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Filler,
  Tooltip,
  Legend,
} from 'chart.js';
import { Line } from 'react-chartjs-2';
import {
  deleteAgent,
  getAgentContainers,
  getContainerHistory,
  renameAgent,
  setAgentAlerts,
  type Agent,
  type Container,
  type ContainerPoint,
  type HistoryRange,
} from '../api/client';
import LogPanel from './LogPanel';
import AlertRuleForm from './AlertRuleForm';
import EndpointsPanel from './EndpointsPanel';

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Filler, Tooltip, Legend);

// Preset ranges in minutes. Anything not in the list is "custom" in the UI.
const RANGE_PRESETS: readonly number[] = [10, 30, 60, 180, 360, 720, 1440, 4320, 10080];

// Max gap (ms) between consecutive points before the line breaks.
// 1m rollups means consecutive buckets are 60s apart; allow 2x slack.
const MAX_GAP_MS = 2 * 60 * 1000;

// cpu_percent is docker-stats style where 100% = 1 full core.
// mcore = percent * 10.  500% = 5000 mcore = 5 cores.
const percentToMCore = (pct: number) => pct * 10;

function fmtCPU(mcore: number): string {
  if (mcore < 1000) return `${mcore.toFixed(0)} mCore`;
  return `${(mcore / 1000).toFixed(2)} Core`;
}

function fmtMem(mb: number): string {
  if (mb < 1024) return `${mb.toFixed(0)} MB`;
  return `${(mb / 1024).toFixed(2)} GB`;
}

function fmtMemPair(used: number, limit: number): string {
  return `${fmtMem(used)} / ${fmtMem(limit)}`;
}

function fmtTick(ms: number, minutes: number): string {
  const d = new Date(ms);
  // Pick tick format by span: short spans = clock, multi-day = date.
  if (minutes > 3 * 24 * 60) return d.toLocaleDateString([], { month: 'short', day: 'numeric' });
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function fmtTooltipTime(ms: number): string {
  const d = new Date(ms);
  return d.toLocaleString([], {
    month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit',
  });
}

function fmtRangeLabel(m: number): string {
  if (m < 60) return `${m}m`;
  if (m < 1440) return `${m / 60}h`;
  return `${m / 1440}d`;
}

// Insert `null` points when two samples are further than MAX_GAP_MS apart
// so Chart.js draws a break in the line instead of interpolating across a gap.
function withGaps<T extends { x: number; y: number }>(pts: T[]): (T | { x: number; y: null })[] {
  const out: (T | { x: number; y: null })[] = [];
  for (let i = 0; i < pts.length; i++) {
    if (i > 0 && pts[i].x - pts[i - 1].x > MAX_GAP_MS) {
      out.push({ x: pts[i - 1].x + 1, y: null });
    }
    out.push(pts[i]);
  }
  return out;
}

export type AgentTab = 'overview' | 'alerts' | 'endpoints' | 'settings';

import type { Role } from '../api/client';

export default function AgentDetail({
  agent,
  token,
  role,
  tab,
  onTabChange,
  onBack,
  onExpired,
  onNavigateSettings,
}: {
  agent: Agent;
  token: string;
  role: Role;
  tab: AgentTab;
  onTabChange: (t: AgentTab) => void;
  onBack: () => void;
  onExpired: () => void;
  onNavigateSettings: () => void;
}) {
  const isAdmin = role === 'admin';
  const [containers, setContainers] = useState<Container[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [deleteInput, setDeleteInput] = useState('');
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const [renameOpen, setRenameOpen] = useState(false);
  const [renameInput, setRenameInput] = useState('');
  const [renaming, setRenaming] = useState(false);
  const [renameError, setRenameError] = useState<string | null>(null);
  const [localHostname, setLocalHostname] = useState(agent.hostname);

  const [alertsOn, setAlertsOn] = useState<boolean>(agent.alerts_enabled);
  const [alertSaving, setAlertSaving] = useState(false);
  const [alertError, setAlertError] = useState<string | null>(null);
  const [confirmDisableAlerts, setConfirmDisableAlerts] = useState(false);

  // Enable: instant. Disable: confirmation modal first — destructive enough
  // to merit one click of friction but not a hostname-match drill.
  const requestAlertToggle = useCallback(() => {
    if (alertsOn) {
      setConfirmDisableAlerts(true);
      return;
    }
    void (async () => {
      setAlertSaving(true);
      setAlertError(null);
      try {
        await setAgentAlerts(token, agent.id, true);
        setAlertsOn(true);
      } catch (err) {
        if (err instanceof Error && err.message === 'Session expired') {
          onExpired();
          return;
        }
        setAlertError(err instanceof Error ? err.message : 'update failed');
      } finally {
        setAlertSaving(false);
      }
    })();
  }, [alertsOn, token, agent.id, onExpired]);

  const confirmDisable = useCallback(async () => {
    setAlertSaving(true);
    setAlertError(null);
    try {
      await setAgentAlerts(token, agent.id, false);
      setAlertsOn(false);
      setConfirmDisableAlerts(false);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') {
        onExpired();
        return;
      }
      setAlertError(err instanceof Error ? err.message : 'update failed');
    } finally {
      setAlertSaving(false);
    }
  }, [token, agent.id, onExpired]);

  // Tab is controlled by the URL via the parent page. Aliased locally so the
  // existing JSX blocks (activeTab === …) don't need renaming.
  const activeTab = tab;
  const pickTab = onTabChange;
  const [rangeMin, setRangeMin] = useState<HistoryRange>(60);
  const [history, setHistory] = useState<ContainerPoint[]>([]);
  const [loadingChart, setLoadingChart] = useState(false);

  const loadContainers = useCallback(async () => {
    try {
      const data = await getAgentContainers(token, agent.id);
      setContainers(data ?? []);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') onExpired();
    }
  }, [token, agent.id, onExpired]);

  const loadHistory = useCallback(async () => {
    if (!selected) return;
    setLoadingChart(true);
    try {
      const data = await getContainerHistory(token, agent.id, selected, rangeMin);
      setHistory(data.points ?? []);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') onExpired();
    } finally {
      setLoadingChart(false);
    }
  }, [token, agent.id, selected, rangeMin, onExpired]);

  useEffect(() => {
    loadContainers();
    const id = setInterval(loadContainers, 10_000);
    return () => clearInterval(id);
  }, [loadContainers]);

  useEffect(() => {
    if (!selected) {
      setHistory([]);
      return;
    }
    loadHistory();
    const id = setInterval(loadHistory, 30_000);
    return () => clearInterval(id);
  }, [loadHistory, selected]);

  // Freeze now-window at each history load so ticks don't drift during hover.
  const nowMs = useMemo(() => Date.now(), [history]);
  const rangeMs = rangeMin * 60_000;
  const xMin = nowMs - rangeMs;
  const xMax = nowMs;

  const cpuMaxMCore = useMemo(
    () => history.reduce((m, p) => Math.max(m, percentToMCore(p.cpu_percent)), 0),
    [history],
  );
  const memMaxMB = useMemo(
    () => history.reduce((m, p) => Math.max(m, p.mem_used_mb), 0),
    [history],
  );
  const cpuInCores = cpuMaxMCore >= 1000;
  const memInGB = memMaxMB >= 1024;

  const cpuPoints = useMemo(
    () =>
      withGaps(
        history.map(p => ({
          x: new Date(p.timestamp).getTime(),
          y: cpuInCores
            ? +(percentToMCore(p.cpu_percent) / 1000).toFixed(3)
            : +percentToMCore(p.cpu_percent).toFixed(0),
        })),
      ),
    [history, cpuInCores],
  );

  const memPoints = useMemo(
    () =>
      withGaps(
        history.map(p => ({
          x: new Date(p.timestamp).getTime(),
          y: memInGB ? +(p.mem_used_mb / 1024).toFixed(3) : +p.mem_used_mb.toFixed(1),
        })),
      ),
    [history, memInGB],
  );

  const sharedXScale = useMemo(
    () => ({
      type: 'linear' as const,
      min: xMin,
      max: xMax,
      ticks: {
        maxTicksLimit: 6,
        maxRotation: 0,
        autoSkip: true,
        callback: (v: string | number) => fmtTick(Number(v), rangeMin),
      },
      grid: { color: 'rgba(128,128,128,0.06)' },
      border: { display: false },
    }),
    [xMin, xMax, rangeMin],
  );

  const cpuChart = useMemo(
    () => ({
      data: {
        datasets: [
          {
            label: cpuInCores ? 'CPU (Core)' : 'CPU (mCore)',
            data: cpuPoints,
            parsing: false as const,
            borderColor: '#5794f2',
            backgroundColor: 'rgba(87,148,242,0.14)',
            fill: true,
            tension: 0.25,
            pointRadius: 0,
            pointHoverRadius: 3,
            borderWidth: 1.5,
            spanGaps: false,
          },
        ],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        animation: false as const,
        interaction: { mode: 'index' as const, intersect: false },
        scales: {
          y: {
            beginAtZero: true,
            ticks: {
              callback: (v: string | number) =>
                cpuInCores ? `${Number(v).toFixed(2)}` : `${v}m`,
              maxTicksLimit: 5,
            },
            grid: { color: 'rgba(128,128,128,0.1)' },
            border: { display: false },
          },
          x: sharedXScale,
        },
        plugins: {
          legend: { position: 'top' as const, labels: { boxWidth: 12, padding: 12 } },
          tooltip: {
            callbacks: {
              title: (items: { parsed: { x: number | null } }[]) => {
                const x = items[0]?.parsed.x;
                return x == null ? '' : fmtTooltipTime(x);
              },
              label: (ctx: { parsed: { y: number | null } }) => {
                const v = ctx.parsed.y ?? 0;
                return cpuInCores ? `${v.toFixed(2)} Core` : `${v.toFixed(0)} mCore`;
              },
            },
          },
        },
      },
    }),
    [cpuPoints, cpuInCores, sharedXScale],
  );

  const memChart = useMemo(
    () => ({
      data: {
        datasets: [
          {
            label: memInGB ? 'RAM (GB)' : 'RAM (MB)',
            data: memPoints,
            parsing: false as const,
            borderColor: '#73bf69',
            backgroundColor: 'rgba(115,191,105,0.14)',
            fill: true,
            tension: 0.25,
            pointRadius: 0,
            pointHoverRadius: 3,
            borderWidth: 1.5,
            spanGaps: false,
          },
        ],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        animation: false as const,
        interaction: { mode: 'index' as const, intersect: false },
        scales: {
          y: {
            beginAtZero: true,
            ticks: {
              callback: (v: string | number) =>
                memInGB ? `${Number(v).toFixed(2)}` : `${v}`,
              maxTicksLimit: 5,
            },
            grid: { color: 'rgba(128,128,128,0.1)' },
            border: { display: false },
          },
          x: sharedXScale,
        },
        plugins: {
          legend: { position: 'top' as const, labels: { boxWidth: 12, padding: 12 } },
          tooltip: {
            callbacks: {
              title: (items: { parsed: { x: number | null } }[]) => {
                const x = items[0]?.parsed.x;
                return x == null ? '' : fmtTooltipTime(x);
              },
              label: (ctx: { parsed: { y: number | null } }) => {
                const v = ctx.parsed.y ?? 0;
                return memInGB ? `${v.toFixed(2)} GB` : `${v.toFixed(0)} MB`;
              },
            },
          },
        },
      },
    }),
    [memPoints, memInGB, sharedXScale],
  );

  const toggleSelect = useCallback(
    (name: string) => setSelected(prev => (prev === name ? null : name)),
    [],
  );

  const openDelete = useCallback(() => {
    setDeleteInput('');
    setDeleteError(null);
    setConfirmDelete(true);
  }, []);

  const closeDelete = useCallback(() => {
    if (deleting) return;
    setConfirmDelete(false);
  }, [deleting]);

  const doDelete = useCallback(async () => {
    if (deleteInput.trim() !== localHostname) return;
    setDeleting(true);
    setDeleteError(null);
    try {
      await deleteAgent(token, agent.id);
      setConfirmDelete(false);
      onBack();
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') {
        onExpired();
        return;
      }
      setDeleteError(err instanceof Error ? err.message : 'delete failed');
    } finally {
      setDeleting(false);
    }
  }, [localHostname, agent.id, deleteInput, token, onBack, onExpired]);

  const openRename = useCallback(() => {
    setRenameInput(localHostname);
    setRenameError(null);
    setRenameOpen(true);
  }, [localHostname]);

  const closeRename = useCallback(() => {
    if (renaming) return;
    setRenameOpen(false);
  }, [renaming]);

  const doRename = useCallback(async () => {
    const next = renameInput.trim();
    if (!next || next === localHostname) {
      setRenameOpen(false);
      return;
    }
    setRenaming(true);
    setRenameError(null);
    try {
      const res = await renameAgent(token, agent.id, next);
      setLocalHostname(res.hostname);
      setRenameOpen(false);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') {
        onExpired();
        return;
      }
      setRenameError(err instanceof Error ? err.message : 'rename failed');
    } finally {
      setRenaming(false);
    }
  }, [renameInput, localHostname, token, agent.id, onExpired]);

  const onRowKey = useCallback(
    (e: KeyboardEvent<HTMLTableRowElement>, name: string) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        toggleSelect(name);
      }
    },
    [toggleSelect],
  );

  return (
    <>
      <div className="agent-context-bar">
        <nav className="breadcrumb" aria-label="Breadcrumb">
          <ol>
            <li>
              <button type="button" className="breadcrumb-link" onClick={onBack}>
                Agents
              </button>
            </li>
            <li aria-current="page" className="breadcrumb-current">
              <span className="breadcrumb-sep" aria-hidden="true">/</span>
              <span className="breadcrumb-hostname">{localHostname}</span>
              <span className="agent-id">{agent.id}</span>
            </li>
          </ol>
        </nav>
        <span className={`alert-pill${alertsOn ? ' on' : ''}`} aria-live="polite">
          <svg width="13" height="13" viewBox="0 0 24 24" fill={alertsOn ? 'currentColor' : 'none'} stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
            <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
          </svg>
          Alerts <strong>{alertsOn ? 'ON' : 'OFF'}</strong>
        </span>
      </div>

      <nav className="agent-tabs" role="tablist" aria-label="Agent sections">
        <button
          type="button"
          role="tab"
          aria-selected={activeTab === 'overview'}
          className={`agent-tab${activeTab === 'overview' ? ' active' : ''}`}
          onClick={() => pickTab('overview')}
        >
          Overview
        </button>
        {isAdmin && (
          <>
            <button
              type="button"
              role="tab"
              aria-selected={activeTab === 'alerts'}
              className={`agent-tab${activeTab === 'alerts' ? ' active' : ''}`}
              onClick={() => pickTab('alerts')}
            >
              Alerts
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={activeTab === 'endpoints'}
              className={`agent-tab${activeTab === 'endpoints' ? ' active' : ''}`}
              onClick={() => pickTab('endpoints')}
            >
              Endpoints
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={activeTab === 'settings'}
              className={`agent-tab${activeTab === 'settings' ? ' active' : ''}`}
              onClick={() => pickTab('settings')}
            >
              Settings
            </button>
          </>
        )}
      </nav>

      <main className="dash-main">
        {activeTab === 'overview' && (
        <>
        <div className="detail-section">
          <div className="chart-header">
            <span className="detail-section-title">Containers</span>
            <span className="detail-subtitle">
              {containers.length} running · refreshes every 10 s · select a row for history
            </span>
          </div>

          {containers.length === 0 ? (
            <div className="dash-empty">No container data yet.</div>
          ) : (
            <div className="table-wrap">
              <table className="container-table" aria-label="Containers">
                <thead>
                  <tr>
                    <th scope="col">Name</th>
                    <th scope="col">Image</th>
                    <th scope="col">CPU</th>
                    <th scope="col">Memory</th>
                    <th scope="col">Status</th>
                  </tr>
                </thead>
                <tbody>
                  {containers.map(c => {
                    const isSel = selected === c.name;
                    return (
                      <tr
                        key={c.id}
                        className={`row-clickable${isSel ? ' row-selected' : ''}`}
                        onClick={() => toggleSelect(c.name)}
                        onKeyDown={e => onRowKey(e, c.name)}
                        tabIndex={0}
                        role="button"
                        aria-pressed={isSel}
                        aria-label={`Container ${c.name}, ${isSel ? 'selected' : 'not selected'}`}
                      >
                        <td className="td-name">{c.name}</td>
                        <td className="td-muted">{c.image}</td>
                        <td>
                          <span className={`cpu-badge ${c.cpu_percent >= 80 ? 'high' : c.cpu_percent >= 40 ? 'mid' : ''}`}>
                            {fmtCPU(percentToMCore(c.cpu_percent))}
                          </span>
                        </td>
                        <td className="td-muted">{fmtMemPair(c.mem_used_mb, c.mem_limit_mb)}</td>
                        <td>
                          <span className={`ctr-status ${c.status.toLowerCase() === 'running' ? 'up' : 'down'}`}>
                            {c.status}
                          </span>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>

        {selected && (
          <div className="detail-section">
            <div className="chart-header">
              <span className="detail-section-title">{selected}</span>
              <label className="log-range-label" aria-label="Time range">
                Last
                <select
                  className="form-input log-range-select"
                  value={String(rangeMin)}
                  onChange={e => setRangeMin(parseInt(e.target.value, 10))}
                >
                  {RANGE_PRESETS.map(m => (
                    <option key={m} value={m}>{fmtRangeLabel(m)}</option>
                  ))}
                </select>
              </label>
            </div>

            <div className="chart-grid">
              <figure className="chart-card">
                <figcaption className="chart-card-title">CPU</figcaption>
                <div className="chart-wrap">
                  {loadingChart ? (
                    <div className="chart-placeholder">Loading…</div>
                  ) : history.length === 0 ? (
                    <div className="chart-placeholder">No data in this range.</div>
                  ) : (
                    <Line data={cpuChart.data} options={cpuChart.options} />
                  )}
                </div>
              </figure>

              <figure className="chart-card">
                <figcaption className="chart-card-title">RAM</figcaption>
                <div className="chart-wrap">
                  {loadingChart ? (
                    <div className="chart-placeholder">Loading…</div>
                  ) : history.length === 0 ? (
                    <div className="chart-placeholder">No data in this range.</div>
                  ) : (
                    <Line data={memChart.data} options={memChart.options} />
                  )}
                </div>
              </figure>
            </div>
          </div>
        )}

        {selected && (
          <LogPanel
            token={token}
            agentId={agent.id}
            containerName={selected}
            onExpired={onExpired}
          />
        )}
        </>
        )}

        {activeTab === 'alerts' && (
          <div className="detail-section" id="alerts">
            <div className="chart-header">
              <div>
                <span className="detail-section-title">Alerts</span>
                <div className="detail-subtitle" style={{ marginTop: 6 }}>
                  {alertsOn
                    ? 'This agent fires configured alerts when thresholds are breached.'
                    : 'Alerts are off. No notifications fire for this agent.'}
                </div>
              </div>
              <button
                type="button"
                className={alertsOn ? 'btn-danger-solid' : 'btn-primary'}
                onClick={requestAlertToggle}
                disabled={alertSaving}
                aria-pressed={alertsOn}
              >
                {alertSaving
                  ? 'Saving…'
                  : alertsOn ? 'Disable alerts' : 'Enable alerts'}
              </button>
            </div>
            {alertError && (
              <div className="login-error" style={{ marginTop: 12 }}>{alertError}</div>
            )}
            {alertsOn && (
              <div style={{ marginTop: 20 }}>
                <AlertRuleForm
                  token={token}
                  agentId={agent.id}
                  onExpired={onExpired}
                  onNavigateSettings={onNavigateSettings}
                />
              </div>
            )}
          </div>
        )}

        {activeTab === 'endpoints' && (
          <EndpointsPanel
            token={token}
            agentId={agent.id}
            onExpired={onExpired}
          />
        )}

        {activeTab === 'settings' && (
          <>
            <div className="detail-section">
              <div className="chart-header">
                <span className="detail-section-title">Rename agent</span>
              </div>
              <p className="settings-hint" style={{ marginTop: 0 }}>
                Hostnames must be unique. Changing the name has no effect on
                the agent token or its collected history.
              </p>
              <button
                type="button"
                className="btn-secondary"
                onClick={openRename}
              >
                Rename…
              </button>
            </div>

            <div className="detail-section danger-zone">
              <div className="chart-header">
                <span className="detail-section-title">Danger zone</span>
              </div>
              <div className="danger-row">
                <div>
                  <div className="danger-row-title">Delete this agent</div>
                  <div className="settings-hint" style={{ marginTop: 4 }}>
                    Permanently removes the agent, all of its stored metrics,
                    and its log history. The agent token stops working
                    immediately. This cannot be undone.
                  </div>
                </div>
                <button
                  type="button"
                  className="btn-danger-solid"
                  onClick={openDelete}
                  aria-label={`Delete agent ${localHostname}`}
                >
                  Delete agent
                </button>
              </div>
            </div>
          </>
        )}
      </main>

      {confirmDisableAlerts && (
        <div
          className="modal-overlay"
          onClick={() => !alertSaving && setConfirmDisableAlerts(false)}
          role="dialog"
          aria-modal="true"
          aria-labelledby="disable-alerts-title"
        >
          <div className="modal" onClick={e => e.stopPropagation()}>
            <h3 className="modal-title" id="disable-alerts-title">Disable alerts</h3>
            <p className="modal-text">
              This turns off all notifications for <strong>{localHostname}</strong>.
              No webhooks will fire even if thresholds are breached. You can
              re-enable at any time.
            </p>
            {alertError && (
              <div className="login-error" style={{ marginBottom: 12 }}>{alertError}</div>
            )}
            <div className="modal-actions">
              <button
                type="button"
                className="btn-secondary"
                onClick={() => setConfirmDisableAlerts(false)}
                disabled={alertSaving}
              >
                Cancel
              </button>
              <button
                type="button"
                className="btn-danger-solid"
                onClick={confirmDisable}
                disabled={alertSaving}
              >
                {alertSaving ? 'Disabling…' : 'Disable alerts'}
              </button>
            </div>
          </div>
        </div>
      )}

      {renameOpen && (
        <div
          className="modal-overlay"
          onClick={closeRename}
          role="dialog"
          aria-modal="true"
          aria-labelledby="rename-agent-title"
        >
          <div className="modal" onClick={e => e.stopPropagation()}>
            <h3 className="modal-title" id="rename-agent-title">Rename agent</h3>
            <p className="modal-text">Hostnames must be unique across agents.</p>
            <input
              type="text"
              className="form-input"
              value={renameInput}
              onChange={e => setRenameInput(e.target.value)}
              placeholder="New hostname"
              aria-label="New hostname"
              autoFocus
              maxLength={128}
              style={{ width: '100%' }}
              onKeyDown={e => { if (e.key === 'Enter') doRename(); }}
            />
            {renameError && (
              <div className="login-error" style={{ marginTop: 12 }}>{renameError}</div>
            )}
            <div className="modal-actions">
              <button type="button" className="btn-secondary" onClick={closeRename} disabled={renaming}>
                Cancel
              </button>
              <button
                type="button"
                className="btn-primary"
                onClick={doRename}
                disabled={renaming || !renameInput.trim() || renameInput.trim() === localHostname}
              >
                {renaming ? 'Saving…' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      )}

      {confirmDelete && (
        <div
          className="modal-overlay"
          onClick={closeDelete}
          role="dialog"
          aria-modal="true"
          aria-labelledby="delete-agent-title"
        >
          <div className="modal" onClick={e => e.stopPropagation()}>
            <h3 className="modal-title" id="delete-agent-title">Delete agent</h3>
            <p className="modal-text">
              This will permanently remove <strong>{localHostname}</strong>, all
              of its stored metrics, and its log history. The agent token will
              stop working immediately. This cannot be undone.
            </p>
            <p className="modal-text">
              Type <code className="mono">{localHostname}</code> to confirm.
            </p>
            <input
              type="text"
              className="form-input"
              value={deleteInput}
              onChange={e => setDeleteInput(e.target.value)}
              placeholder={localHostname}
              aria-label="Type hostname to confirm deletion"
              autoFocus
              style={{ width: '100%' }}
            />
            {deleteError && (
              <div className="login-error" style={{ marginTop: 12 }}>{deleteError}</div>
            )}
            <div className="modal-actions">
              <button
                type="button"
                className="btn-secondary"
                onClick={closeDelete}
                disabled={deleting}
              >
                Cancel
              </button>
              <button
                type="button"
                className="btn-danger-solid"
                onClick={doDelete}
                disabled={deleting || deleteInput.trim() !== agent.hostname}
              >
                {deleting ? 'Deleting…' : 'Delete agent'}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
