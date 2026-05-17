import { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import SingleSelectDropdown from './SingleSelect';
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
  getAgent,
  getAgentContainers,
  getContainerHistory,
  renameAgent,
  setAgentAlerts,
  listProjects,
  moveAgentToProject,
  type Agent,
  type Container,
  type ContainerPoint,
  type HistoryRange,
  type Project,
} from '../api/client';
import LogPanel from './LogPanel';
import AlertRuleForm from './AlertRuleForm';
import EndpointsPanel from './EndpointsPanel';

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Filler, Tooltip, Legend);

const RANGE_PRESETS: readonly number[] = [10, 30, 60, 180, 360, 720, 1440, 4320, 10080];
const MAX_GAP_MS = 2 * 60 * 1000;

const CHART_COLORS = [
  { line: '#5794f2', fill: 'rgba(87,148,242,0.12)' },
  { line: '#73bf69', fill: 'rgba(115,191,105,0.12)' },
  { line: '#f2cc0c', fill: 'rgba(242,204,12,0.12)' },
  { line: '#ff780a', fill: 'rgba(255,120,10,0.12)' },
  { line: '#fa7fd4', fill: 'rgba(250,127,212,0.12)' },
  { line: '#a352cc', fill: 'rgba(163,82,204,0.12)' },
  { line: '#1f9ed2', fill: 'rgba(31,158,210,0.12)' },
  { line: '#e02f44', fill: 'rgba(224,47,68,0.12)' },
];

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
  if (minutes > 3 * 24 * 60) return d.toLocaleDateString([], { month: 'short', day: 'numeric' });
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function fmtTooltipTime(ms: number): string {
  const d = new Date(ms);
  return d.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

function fmtRangeLabel(m: number): string {
  if (m < 60) return `${m}m`;
  if (m < 1440) return `${m / 60}h`;
  return `${m / 1440}d`;
}

function barColor(pct: number): string {
  if (pct >= 80) return 'var(--err)';
  if (pct >= 60) return 'var(--warn)';
  return 'var(--ok)';
}

function fmtAge(ts: string | null): string {
  if (!ts) return 'never';
  const diff = Date.now() - new Date(ts).getTime();
  const s = Math.floor(diff / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

function fmtUptime(ts: string): string {
  if (!ts) return '—';
  const diff = Date.now() - new Date(ts).getTime();
  if (diff < 0) return '—';
  const s = Math.floor(diff / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ${m % 60}m`;
  return `${Math.floor(h / 24)}d ${h % 24}h`;
}


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


// Multi-select dropdown for container picker in Metrics tab.
function MultiSelect({ options, selected, onChange }: {
  options: string[];
  selected: string[];
  onChange: (v: string[]) => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const ref = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  useEffect(() => {
    if (open) {
      setQuery('');
      requestAnimationFrame(() => searchRef.current?.focus());
    }
  }, [open]);

  const toggle = (name: string) =>
    onChange(selected.includes(name) ? selected.filter(n => n !== name) : [...selected, name]);

  const showSearch = options.length > 6;
  const filtered = useMemo(() => {
    if (!query.trim()) return options;
    const q = query.toLowerCase();
    return options.filter(o => o.toLowerCase().includes(q));
  }, [options, query]);

  const label = selected.length === 0
    ? 'Select containers…'
    : selected.length === 1
      ? selected[0]
      : `${selected.length} containers selected`;

  return (
    <div ref={ref} style={{ position: 'relative', display: 'inline-block' }}>
      <button
        type="button"
        className={`multiselect-btn${open ? ' open' : ''}`}
        onClick={() => setOpen(o => !o)}
      >
        <span style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{label}</span>
        <span className="chevron">▾</span>
      </button>
      {open && (
        <div className="multiselect-dropdown">
          {showSearch && (
            <div style={{ padding: '6px 8px', borderBottom: '1px solid var(--border)' }}>
              <input
                ref={searchRef}
                type="text"
                value={query}
                onChange={e => setQuery(e.target.value)}
                onKeyDown={e => { if (e.key === 'Escape') setOpen(false); }}
                placeholder="Search…"
                style={{
                  width: '100%', padding: '6px 8px',
                  border: '1px solid var(--border)', borderRadius: 'var(--r)',
                  background: 'var(--canvas)', color: 'var(--text)',
                  fontFamily: 'var(--f-ui)', fontSize: 12, outline: 'none',
                }}
              />
            </div>
          )}
          {filtered.length === 0 && (
            <div className="multiselect-empty">
              {options.length === 0 ? 'No containers' : 'No matches'}
            </div>
          )}
          {filtered.map(name => (
            <label key={name} className="multiselect-option">
              <input
                type="checkbox"
                checked={selected.includes(name)}
                onChange={() => toggle(name)}
              />
              <span>{name}</span>
            </label>
          ))}
          {selected.length > 0 && (
            <button
              type="button"
              className="multiselect-clear"
              onClick={() => { onChange([]); setOpen(false); }}
            >
              Clear all
            </button>
          )}
        </div>
      )}
    </div>
  );
}

export type AgentTab = 'overview' | 'metrics' | 'logs' | 'alerts' | 'endpoints' | 'settings';

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
  const activeTab = tab;
  const pickTab = onTabChange;

  // ── Container list (overview) ────────────────────────────────────────────
  const [containers, setContainers] = useState<Container[]>([]);
  const [sortKey, setSortKey] = useState<'name' | 'cpu' | 'mem'>('name');
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc');
  const [refreshMs, setRefreshMs] = useState(10_000);
  const [customInput, setCustomInput] = useState('');
  const [customApplied, setCustomApplied] = useState(false);

  // ── Metrics tab ──────────────────────────────────────────────────────────
  const [metricsSelected, setMetricsSelected] = useState<string[]>([]);
  const [metricsHistory, setMetricsHistory] = useState<Record<string, ContainerPoint[]>>({});
  const [metricsRange, setMetricsRange] = useState<HistoryRange>(60);
  const [metricsLoading, setMetricsLoading] = useState(false);

  // ── Logs tab ─────────────────────────────────────────────────────────────
  const [logsContainer, setLogsContainer] = useState<string | null>(null);

  // ── Agent settings ───────────────────────────────────────────────────────
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

  // ── Move-to-project ──────────────────────────────────────────────────────
  const [moveOpen, setMoveOpen] = useState(false);
  const [moveProjects, setMoveProjects] = useState<Project[]>([]);
  const [moveTarget, setMoveTarget] = useState<string>('');
  const [moving, setMoving] = useState(false);
  const [moveError, setMoveError] = useState<string | null>(null);

  useEffect(() => {
    if (!moveOpen) return;
    listProjects()
      .then(list => {
        setMoveProjects(list);
        setMoveTarget('');
        setMoveError(null);
      })
      .catch(err => setMoveError(err instanceof Error ? err.message : 'Failed to load projects'));
  }, [moveOpen]);

  const doMove = useCallback(async () => {
    const pid = Number(moveTarget);
    if (!pid || pid === agent.project_id) {
      setMoveError('Pick a different project');
      return;
    }
    setMoving(true);
    setMoveError(null);
    try {
      await moveAgentToProject(agent.id, pid);
      setMoveOpen(false);
      // Reload to reflect new project_name in header
      window.location.reload();
    } catch (err) {
      setMoveError(err instanceof Error ? err.message : 'Move failed');
    } finally {
      setMoving(false);
    }
  }, [agent.id, agent.project_id, moveTarget]);

  // ── Live agent state (host metrics, refreshed at container interval) ────────
  const [liveAgent, setLiveAgent] = useState<Agent>(agent);

  const loadAgent = useCallback(async () => {
    try {
      const data = await getAgent(token, agent.id);
      setLiveAgent(data);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') onExpired();
    }
  }, [token, agent.id, onExpired]);

  // ── Load containers (overview list + dropdown options) ───────────────────
  const loadContainers = useCallback(async () => {
    try {
      const data = await getAgentContainers(token, agent.id);
      setContainers(data ?? []);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') onExpired();
    }
  }, [token, agent.id, onExpired]);

  useEffect(() => {
    loadAgent();
    loadContainers();
    const id = setInterval(() => { void loadAgent(); void loadContainers(); }, refreshMs);
    return () => clearInterval(id);
  }, [loadAgent, loadContainers, refreshMs]);

  // ── Load metrics for selected containers ─────────────────────────────────
  const loadMetrics = useCallback(async () => {
    if (metricsSelected.length === 0) { setMetricsHistory({}); return; }
    setMetricsLoading(true);
    try {
      const results = await Promise.all(
        metricsSelected.map(name =>
          getContainerHistory(token, agent.id, name, metricsRange)
            .then(d => ({ name, points: d.points ?? [] as ContainerPoint[] }))
            .catch(() => ({ name, points: [] as ContainerPoint[] }))
        )
      );
      setMetricsHistory(() => {
        const next: Record<string, ContainerPoint[]> = {};
        for (const { name, points } of results) next[name] = points;
        return next;
      });
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') onExpired();
    } finally {
      setMetricsLoading(false);
    }
  }, [token, agent.id, metricsSelected, metricsRange, onExpired]);

  useEffect(() => {
    if (activeTab !== 'metrics') return;
    loadMetrics();
    const id = setInterval(loadMetrics, 30_000);
    return () => clearInterval(id);
  }, [loadMetrics, activeTab]);

  // ── Sorted containers ────────────────────────────────────────────────────
  const sortedContainers = useMemo(() => {
    const dir = sortDir === 'asc' ? 1 : -1;
    return [...containers].sort((a, b) => {
      let d = 0;
      if (sortKey === 'cpu') d = a.cpu_percent - b.cpu_percent;
      else if (sortKey === 'mem') d = a.mem_used_mb - b.mem_used_mb;
      else d = a.name.localeCompare(b.name);
      if (d !== 0) return d * dir;
      return a.name.localeCompare(b.name);
    });
  }, [containers, sortKey, sortDir]);

  const toggleSort = (key: typeof sortKey) => {
    if (sortKey === key) setSortDir(d => d === 'asc' ? 'desc' : 'asc');
    else { setSortKey(key); setSortDir(key === 'name' ? 'asc' : 'desc'); }
  };

  const containerNames = useMemo(() => containers.map(c => c.name).sort(), [containers]);

  const runningCount = useMemo(() => containers.filter(c => c.status.toLowerCase() === 'running').length, [containers]);
  const stoppedCount = containers.length - runningCount;

  // ── Chart computation ─────────────────────────────────────────────────────
  const nowMs = useMemo(() => Date.now(), [metricsHistory]);
  const rangeMs = metricsRange * 60_000;
  const xMin = nowMs - rangeMs;
  const xMax = nowMs;

  const allPoints = useMemo(
    () => Object.values(metricsHistory).flat(),
    [metricsHistory],
  );
  const cpuMaxMCore = useMemo(
    () => allPoints.reduce((m, p) => Math.max(m, percentToMCore(p.cpu_percent)), 0),
    [allPoints],
  );
  const memMaxMB = useMemo(
    () => allPoints.reduce((m, p) => Math.max(m, p.mem_used_mb), 0),
    [allPoints],
  );
  const cpuInCores = cpuMaxMCore >= 1000;
  const memInGB = memMaxMB >= 1024;

  const TICK_COLOR = 'rgba(144,152,161,0.7)';
  const GRID_COLOR = 'rgba(255,255,255,0.04)';
  const TOOLTIP_STYLE = {
    backgroundColor: 'rgba(14,16,22,0.95)',
    titleColor: '#d8d9da',
    bodyColor: '#9098a1',
    borderColor: 'rgba(255,255,255,0.08)',
    borderWidth: 1,
    padding: 10,
    cornerRadius: 4,
    displayColors: true,
    boxWidth: 10,
    boxHeight: 10,
  } as const;

  const sharedXScale = useMemo(() => ({
    type: 'linear' as const,
    min: xMin,
    max: xMax,
    ticks: {
      maxTicksLimit: 6,
      maxRotation: 0,
      autoSkip: true,
      color: TICK_COLOR,
      font: { size: 11 },
      callback: (v: string | number) => fmtTick(Number(v), metricsRange),
    },
    grid: { color: GRID_COLOR },
    border: { display: false },
  }), [xMin, xMax, metricsRange]);

  const cpuChart = useMemo(() => ({
    data: {
      datasets: metricsSelected.map((name, i) => {
        const c = CHART_COLORS[i % CHART_COLORS.length];
        const pts = (metricsHistory[name] ?? []).map(p => ({
          x: new Date(p.timestamp).getTime(),
          y: cpuInCores
            ? +(percentToMCore(p.cpu_percent) / 1000).toFixed(3)
            : +percentToMCore(p.cpu_percent).toFixed(0),
        }));
        return {
          label: name,
          data: withGaps(pts),
          parsing: false as const,
          borderColor: c.line,
          backgroundColor: c.fill,
          fill: metricsSelected.length === 1,
          tension: 0.35,
          pointRadius: 0,
          pointHoverRadius: 5,
          pointHoverBackgroundColor: c.line,
          borderWidth: 2,
          spanGaps: false,
        };
      }),
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
            color: TICK_COLOR,
            font: { size: 11 },
            callback: (v: string | number) =>
              cpuInCores ? `${Number(v).toFixed(2)}` : `${v}m`,
            maxTicksLimit: 5,
          },
          grid: { color: GRID_COLOR },
          border: { display: false },
        },
        x: sharedXScale,
      },
      plugins: {
        legend: {
          position: 'top' as const,
          labels: { boxWidth: 10, boxHeight: 10, padding: 16, color: '#9098a1', font: { size: 12 } },
        },
        tooltip: {
          ...TOOLTIP_STYLE,
          callbacks: {
            title: (items: { parsed: { x: number | null } }[]) => {
              const x = items[0]?.parsed.x;
              return x == null ? '' : fmtTooltipTime(x);
            },
            label: (ctx: { dataset: { label?: string }; parsed: { y: number | null } }) => {
              const v = ctx.parsed.y ?? 0;
              const val = cpuInCores ? `${v.toFixed(2)} Core` : `${v.toFixed(0)} mCore`;
              return `  ${ctx.dataset.label}: ${val}`;
            },
          },
        },
      },
    },
  }), [metricsSelected, metricsHistory, cpuInCores, sharedXScale]);

  const memChart = useMemo(() => ({
    data: {
      datasets: metricsSelected.map((name, i) => {
        const c = CHART_COLORS[i % CHART_COLORS.length];
        const pts = (metricsHistory[name] ?? []).map(p => ({
          x: new Date(p.timestamp).getTime(),
          y: memInGB ? +(p.mem_used_mb / 1024).toFixed(3) : +p.mem_used_mb.toFixed(1),
        }));
        return {
          label: name,
          data: withGaps(pts),
          parsing: false as const,
          borderColor: c.line,
          backgroundColor: c.fill,
          fill: metricsSelected.length === 1,
          tension: 0.35,
          pointRadius: 0,
          pointHoverRadius: 5,
          pointHoverBackgroundColor: c.line,
          borderWidth: 2,
          spanGaps: false,
        };
      }),
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
            color: TICK_COLOR,
            font: { size: 11 },
            callback: (v: string | number) =>
              memInGB ? `${Number(v).toFixed(2)}` : `${v}`,
            maxTicksLimit: 5,
          },
          grid: { color: GRID_COLOR },
          border: { display: false },
        },
        x: sharedXScale,
      },
      plugins: {
        legend: {
          position: 'top' as const,
          labels: { boxWidth: 10, boxHeight: 10, padding: 16, color: '#9098a1', font: { size: 12 } },
        },
        tooltip: {
          ...TOOLTIP_STYLE,
          callbacks: {
            title: (items: { parsed: { x: number | null } }[]) => {
              const x = items[0]?.parsed.x;
              return x == null ? '' : fmtTooltipTime(x);
            },
            label: (ctx: { dataset: { label?: string }; parsed: { y: number | null } }) => {
              const v = ctx.parsed.y ?? 0;
              const val = memInGB ? `${v.toFixed(2)} GB` : `${v.toFixed(0)} MB`;
              return `  ${ctx.dataset.label}: ${val}`;
            },
          },
        },
      },
    },
  }), [metricsSelected, metricsHistory, memInGB, sharedXScale]);

  // ── Agent actions ─────────────────────────────────────────────────────────
  const requestAlertToggle = useCallback(() => {
    if (alertsOn) { setConfirmDisableAlerts(true); return; }
    void (async () => {
      setAlertSaving(true); setAlertError(null);
      try {
        await setAgentAlerts(token, agent.id, true);
        setAlertsOn(true);
      } catch (err) {
        if (err instanceof Error && err.message === 'Session expired') { onExpired(); return; }
        setAlertError(err instanceof Error ? err.message : 'update failed');
      } finally { setAlertSaving(false); }
    })();
  }, [alertsOn, token, agent.id, onExpired]);

  const confirmDisable = useCallback(async () => {
    setAlertSaving(true); setAlertError(null);
    try {
      await setAgentAlerts(token, agent.id, false);
      setAlertsOn(false); setConfirmDisableAlerts(false);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') { onExpired(); return; }
      setAlertError(err instanceof Error ? err.message : 'update failed');
    } finally { setAlertSaving(false); }
  }, [token, agent.id, onExpired]);

  const openDelete = useCallback(() => { setDeleteInput(''); setDeleteError(null); setConfirmDelete(true); }, []);
  const closeDelete = useCallback(() => { if (deleting) return; setConfirmDelete(false); }, [deleting]);

  const doDelete = useCallback(async () => {
    if (deleteInput.trim() !== localHostname) return;
    setDeleting(true); setDeleteError(null);
    try {
      await deleteAgent(token, agent.id);
      setConfirmDelete(false); onBack();
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') { onExpired(); return; }
      setDeleteError(err instanceof Error ? err.message : 'delete failed');
    } finally { setDeleting(false); }
  }, [localHostname, agent.id, deleteInput, token, onBack, onExpired]);

  const openRename = useCallback(() => { setRenameInput(localHostname); setRenameError(null); setRenameOpen(true); }, [localHostname]);
  const closeRename = useCallback(() => { if (renaming) return; setRenameOpen(false); }, [renaming]);

  const doRename = useCallback(async () => {
    const next = renameInput.trim();
    if (!next || next === localHostname) { setRenameOpen(false); return; }
    setRenaming(true); setRenameError(null);
    try {
      const res = await renameAgent(token, agent.id, next);
      setLocalHostname(res.hostname); setRenameOpen(false);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') { onExpired(); return; }
      setRenameError(err instanceof Error ? err.message : 'rename failed');
    } finally { setRenaming(false); }
  }, [renameInput, localHostname, token, agent.id, onExpired]);

  // ── Render ────────────────────────────────────────────────────────────────
  return (
    <>
      <div className="agent-context-bar">
        <nav className="breadcrumb" aria-label="Breadcrumb">
          <ol>
            <li>
              <button type="button" className="breadcrumb-link" onClick={onBack}>Agents</button>
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
        {(['overview', 'metrics', 'logs'] as AgentTab[]).map(t => (
          <button
            key={t}
            type="button"
            role="tab"
            aria-selected={activeTab === t}
            className={`agent-tab${activeTab === t ? ' active' : ''}`}
            onClick={() => pickTab(t)}
          >
            {t.charAt(0).toUpperCase() + t.slice(1)}
          </button>
        ))}
        {isAdmin && (
          <>
            {(['alerts', 'endpoints', 'settings'] as AgentTab[]).map(t => (
              <button
                key={t}
                type="button"
                role="tab"
                aria-selected={activeTab === t}
                className={`agent-tab${activeTab === t ? ' active' : ''}`}
                onClick={() => pickTab(t)}
              >
                {t.charAt(0).toUpperCase() + t.slice(1)}
              </button>
            ))}
          </>
        )}
      </nav>

      <main className="dash-main">

        {/* ── Overview ── */}
        {activeTab === 'overview' && (
          <>
            {/* Agent health card */}
            <div className="detail-section">
              <div className="chart-header">
                <span className="detail-section-title">Agent Health</span>
                <span className="detail-subtitle">Last seen {fmtAge(liveAgent.last_seen)}</span>
              </div>
              <div className="agent-stats-grid">
                {liveAgent.cpu_percent != null && (
                  <div className="agent-stat-block">
                    <span className="agent-stat-label">Host CPU</span>
                    <span className="agent-stat-val">{liveAgent.cpu_percent.toFixed(1)}<span className="agent-stat-total">%</span></span>
                    <div className="metric-bar-wrap">
                      <div className="metric-bar" style={{ width: `${Math.min(liveAgent.cpu_percent, 100)}%`, background: barColor(liveAgent.cpu_percent) }} />
                    </div>
                  </div>
                )}
                {liveAgent.mem_used_gb != null && liveAgent.mem_total_gb != null && liveAgent.mem_total_gb > 0 && (
                  <div className="agent-stat-block">
                    <span className="agent-stat-label">RAM</span>
                    <span className="agent-stat-val">
                      {liveAgent.mem_used_gb.toFixed(1)}<span className="agent-stat-total"> / {liveAgent.mem_total_gb.toFixed(1)} GB</span>
                    </span>
                    <div className="metric-bar-wrap">
                      <div className="metric-bar" style={{ width: `${Math.min((liveAgent.mem_used_gb / liveAgent.mem_total_gb) * 100, 100)}%`, background: barColor((liveAgent.mem_used_gb / liveAgent.mem_total_gb) * 100) }} />
                    </div>
                  </div>
                )}
                {liveAgent.disk_used_gb != null && liveAgent.disk_total_gb != null && liveAgent.disk_total_gb > 0 && (
                  <div className="agent-stat-block">
                    <span className="agent-stat-label">Disk</span>
                    <span className="agent-stat-val">
                      {liveAgent.disk_used_gb.toFixed(1)}<span className="agent-stat-total"> / {liveAgent.disk_total_gb.toFixed(1)} GB</span>
                    </span>
                    <div className="metric-bar-wrap">
                      <div className="metric-bar" style={{ width: `${Math.min((liveAgent.disk_used_gb / liveAgent.disk_total_gb) * 100, 100)}%`, background: barColor((liveAgent.disk_used_gb / liveAgent.disk_total_gb) * 100) }} />
                    </div>
                  </div>
                )}
                <div className="agent-stat-block">
                  <span className="agent-stat-label">Issues</span>
                  <span className="agent-stat-val" style={{ color: liveAgent.active_issues > 0 ? 'var(--err)' : 'var(--ok)', fontSize: 14 }}>
                    {liveAgent.active_issues > 0 ? `${liveAgent.active_issues} active` : 'none'}
                  </span>
                </div>
              </div>
            </div>

            {/* Containers card */}
            <div className="detail-section">
              <div className="chart-header">
                <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                  <span className="detail-section-title">Containers</span>
                  <span className="detail-subtitle">
                    {containers.length === 0
                      ? '0 containers'
                      : <>
                          {runningCount > 0 && <span style={{ color: 'var(--ok)' }}>{runningCount} running</span>}
                          {runningCount > 0 && stoppedCount > 0 && <span style={{ color: 'var(--muted)' }}> · </span>}
                          {stoppedCount > 0 && <span style={{ color: 'var(--err)' }}>{stoppedCount} stopped</span>}
                        </>
                    }
                  </span>
                </div>
                <span style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                  {[3000, 5000, 10000].map(ms => (
                    <button
                      key={ms}
                      className={`btn btn-xs${refreshMs === ms && !customApplied ? ' btn-active' : ''}`}
                      onClick={() => { setRefreshMs(ms); setCustomApplied(false); setCustomInput(''); }}
                    >{ms / 1000}s</button>
                  ))}
                  <input
                    type="number"
                    min={1}
                    placeholder="custom s"
                    value={customInput}
                    onChange={e => { setCustomInput(e.target.value); setCustomApplied(false); }}
                    onKeyDown={e => {
                      if (e.key === 'Enter') {
                        const n = parseInt(customInput, 10);
                        if (n >= 1) { setRefreshMs(n * 1000); setCustomApplied(true); }
                      }
                    }}
                    style={{ width: 72, fontSize: 11, color: customApplied ? 'var(--muted)' : undefined }}
                    className="input-xs"
                  />
                  <button
                    className="btn btn-xs"
                    onClick={() => {
                      const n = parseInt(customInput, 10);
                      if (n >= 1) { setRefreshMs(n * 1000); setCustomApplied(true); }
                    }}
                  >set</button>
                </span>
              </div>

              {containers.length === 0 ? (
                <div className="dash-empty">No container data yet.</div>
              ) : (
                <div className="table-wrap">
                  <table className="container-table" aria-label="Containers">
                    <thead>
                      <tr>
                        <th scope="col" className={`th-sort${sortKey === 'name' ? ' th-sort-active' : ''}`} onClick={() => toggleSort('name')}>
                          Name {sortKey === 'name' ? (sortDir === 'asc' ? '↑' : '↓') : '↕'}
                        </th>
                        <th scope="col">Image</th>
                        <th scope="col" className={`th-sort${sortKey === 'cpu' ? ' th-sort-active' : ''}`} onClick={() => toggleSort('cpu')}>
                          CPU {sortKey === 'cpu' ? (sortDir === 'asc' ? '↑' : '↓') : '↕'}
                        </th>
                        <th scope="col" className={`th-sort${sortKey === 'mem' ? ' th-sort-active' : ''}`} onClick={() => toggleSort('mem')}>
                          Memory {sortKey === 'mem' ? (sortDir === 'asc' ? '↑' : '↓') : '↕'}
                        </th>
                        <th scope="col">Uptime</th>
                        <th scope="col">Restarts</th>
                        <th scope="col">Status</th>
                      </tr>
                    </thead>
                    <tbody>
                      {sortedContainers.map(c => {
                        const memPct = c.mem_limit_mb > 0 ? (c.mem_used_mb / c.mem_limit_mb) * 100 : 0;
                        return (
                          <tr key={c.id}>
                            <td className="td-name">{c.name}</td>
                            <td className="td-muted">{c.image}</td>
                            <td>
                              <span className={`cpu-badge ${c.cpu_percent >= 80 ? 'high' : c.cpu_percent >= 40 ? 'mid' : ''}`}>
                                {fmtCPU(percentToMCore(c.cpu_percent))}
                              </span>
                            </td>
                            <td>
                              <span className="td-muted">{fmtMemPair(c.mem_used_mb, c.mem_limit_mb)}</span>
                              {c.mem_limit_mb > 0 && (
                                <div className="mem-cell-bar">
                                  <div className="mem-cell-bar-fill" style={{ width: `${Math.min(memPct, 100)}%`, background: barColor(memPct) }} />
                                </div>
                              )}
                            </td>
                            <td className="td-muted">
                              {c.status.toLowerCase() === 'running' && c.started_at
                                ? fmtUptime(c.started_at)
                                : '—'}
                            </td>
                            <td>
                              <span className={c.restart_count > 0 ? 'restart-badge warn' : 'restart-badge'}>
                                {c.restart_count}
                              </span>
                            </td>
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
          </>
        )}

        {/* ── Metrics ── */}
        {activeTab === 'metrics' && (
          <div className="detail-section">
            <div className="chart-header" style={{ flexWrap: 'wrap', gap: 12 }}>
              <span className="detail-section-title">Metrics</span>
              <span style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
                <MultiSelect
                  options={containerNames}
                  selected={metricsSelected}
                  onChange={setMetricsSelected}
                />
                <SingleSelectDropdown
                  options={RANGE_PRESETS.map(m => ({ label: fmtRangeLabel(m), value: String(m) }))}
                  value={String(metricsRange)}
                  onChange={v => setMetricsRange(parseInt(v, 10))}
                  placeholder="Range…"
                />
              </span>
            </div>

            {metricsSelected.length === 0 ? (
              <div className="dash-empty">Select one or more containers to view metrics.</div>
            ) : metricsLoading && Object.keys(metricsHistory).length === 0 ? (
              <div className="chart-placeholder" style={{ padding: 32 }}>Loading…</div>
            ) : (
              <div className="chart-grid">
                <figure className="chart-card cpu">
                  <figcaption className="chart-card-title">CPU</figcaption>
                  <div className="chart-wrap">
                    {allPoints.length === 0 ? (
                      <div className="chart-placeholder">No data in this range.</div>
                    ) : (
                      <Line data={cpuChart.data} options={cpuChart.options} />
                    )}
                  </div>
                </figure>
                <figure className="chart-card ram">
                  <figcaption className="chart-card-title">RAM</figcaption>
                  <div className="chart-wrap">
                    {allPoints.length === 0 ? (
                      <div className="chart-placeholder">No data in this range.</div>
                    ) : (
                      <Line data={memChart.data} options={memChart.options} />
                    )}
                  </div>
                </figure>
              </div>
            )}
          </div>
        )}

        {/* ── Logs ── */}
        {activeTab === 'logs' && (
          <>
            <div className="detail-section">
              <div style={{ display: 'flex', alignItems: 'center', gap: 12, justifyContent: 'space-between' }}>
                <span className="detail-section-title" style={{ whiteSpace: 'nowrap' }}>Container</span>
                <SingleSelectDropdown
                  options={containerNames.map(n => ({ label: n, value: n }))}
                  value={logsContainer}
                  onChange={setLogsContainer}
                  placeholder="Select container…"
                />
              </div>
            </div>
            {logsContainer ? (
              <LogPanel
                token={token}
                agentId={agent.id}
                containerName={logsContainer}
                onExpired={onExpired}
              />
            ) : (
              <div className="dash-empty" style={{ padding: '32px 20px' }}>Select a container above to view logs.</div>
            )}
          </>
        )}

        {/* ── Alerts ── */}
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
                {alertSaving ? 'Saving…' : alertsOn ? 'Disable alerts' : 'Enable alerts'}
              </button>
            </div>
            {alertError && <div className="login-error" style={{ marginTop: 12 }}>{alertError}</div>}
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

        {/* ── Endpoints ── */}
        {activeTab === 'endpoints' && (
          <EndpointsPanel token={token} agentId={agent.id} onExpired={onExpired} />
        )}

        {/* ── Settings ── */}
        {activeTab === 'settings' && (
          <>
            <div className="detail-section">
              <div className="chart-header">
                <span className="detail-section-title">Rename agent</span>
              </div>
              <p className="settings-hint" style={{ marginTop: 0 }}>
                Hostnames must be unique. Changing the name has no effect on the agent token or its collected history.
              </p>
              <button type="button" className="btn-secondary" onClick={openRename}>Rename…</button>
            </div>
            <div className="detail-section">
              <div className="chart-header">
                <span className="detail-section-title">Move to project</span>
              </div>
              <p className="settings-hint" style={{ marginTop: 0 }}>
                Currently in <strong>{agent.project_name}{agent.project_environment ? ` (${agent.project_environment})` : ''}</strong>.
              </p>
              <button type="button" className="btn-secondary" onClick={() => setMoveOpen(true)}>Move…</button>
            </div>
            <div className="detail-section danger-zone">
              <div className="chart-header">
                <span className="detail-section-title">Danger zone</span>
              </div>
              <div className="danger-row">
                <div>
                  <div className="danger-row-title">Delete this agent</div>
                  <div className="settings-hint" style={{ marginTop: 4 }}>
                    Permanently removes the agent, all of its stored metrics, and its log history. The agent token stops working immediately. This cannot be undone.
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

      {/* ── Modals ── */}
      {confirmDisableAlerts && (
        <div className="modal-overlay" onClick={() => !alertSaving && setConfirmDisableAlerts(false)} role="dialog" aria-modal="true" aria-labelledby="disable-alerts-title">
          <div className="modal" onClick={e => e.stopPropagation()}>
            <h3 className="modal-title" id="disable-alerts-title">Disable alerts</h3>
            <p className="modal-text">
              This turns off all notifications for <strong>{localHostname}</strong>. No webhooks will fire even if thresholds are breached.
            </p>
            {alertError && <div className="login-error" style={{ marginBottom: 12 }}>{alertError}</div>}
            <div className="modal-actions">
              <button type="button" className="btn-secondary" onClick={() => setConfirmDisableAlerts(false)} disabled={alertSaving}>Cancel</button>
              <button type="button" className="btn-danger-solid" onClick={confirmDisable} disabled={alertSaving}>
                {alertSaving ? 'Disabling…' : 'Disable alerts'}
              </button>
            </div>
          </div>
        </div>
      )}

      {moveOpen && (
        <div className="modal-overlay" onClick={() => !moving && setMoveOpen(false)} role="dialog" aria-modal="true">
          <div className="modal" onClick={e => e.stopPropagation()}>
            <h3 className="modal-title">Move to another project</h3>
            <p className="modal-text">
              Currently in <strong>{agent.project_name}{agent.project_environment ? ` (${agent.project_environment})` : ''}</strong>.
            </p>
            <select
              className="form-input"
              value={moveTarget}
              onChange={e => setMoveTarget(e.target.value)}
              style={{ width: '100%' }}
            >
              <option value="">— pick a project —</option>
              {moveProjects
                .filter(p => p.id !== agent.project_id)
                .map(p => (
                  <option key={p.id} value={String(p.id)}>
                    {p.name}{p.environment ? ` (${p.environment})` : ''}
                  </option>
                ))}
            </select>
            {moveError && <div className="login-error" style={{ marginTop: 12 }}>{moveError}</div>}
            <div className="modal-actions">
              <button type="button" className="btn-secondary" onClick={() => setMoveOpen(false)} disabled={moving}>Cancel</button>
              <button type="button" className="btn-primary" onClick={doMove} disabled={moving || !moveTarget}>
                {moving ? 'Moving…' : 'Move'}
              </button>
            </div>
          </div>
        </div>
      )}

      {renameOpen && (
        <div className="modal-overlay" onClick={closeRename} role="dialog" aria-modal="true" aria-labelledby="rename-agent-title">
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
            {renameError && <div className="login-error" style={{ marginTop: 12 }}>{renameError}</div>}
            <div className="modal-actions">
              <button type="button" className="btn-secondary" onClick={closeRename} disabled={renaming}>Cancel</button>
              <button type="button" className="btn-primary" onClick={doRename} disabled={renaming || !renameInput.trim() || renameInput.trim() === localHostname}>
                {renaming ? 'Saving…' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      )}

      {confirmDelete && (
        <div className="modal-overlay" onClick={closeDelete} role="dialog" aria-modal="true" aria-labelledby="delete-agent-title">
          <div className="modal" onClick={e => e.stopPropagation()}>
            <h3 className="modal-title" id="delete-agent-title">Delete agent</h3>
            <p className="modal-text">
              This will permanently remove <strong>{localHostname}</strong>, all of its stored metrics, and its log history. The agent token will stop working immediately. This cannot be undone.
            </p>
            <p className="modal-text">Type <code className="mono">{localHostname}</code> to confirm.</p>
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
            {deleteError && <div className="login-error" style={{ marginTop: 12 }}>{deleteError}</div>}
            <div className="modal-actions">
              <button type="button" className="btn-secondary" onClick={closeDelete} disabled={deleting}>Cancel</button>
              <button type="button" className="btn-danger-solid" onClick={doDelete} disabled={deleting || deleteInput.trim() !== agent.hostname}>
                {deleting ? 'Deleting…' : 'Delete agent'}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
