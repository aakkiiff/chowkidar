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
  getAgentContainers,
  getContainerHistory,
  type Agent,
  type Container,
  type ContainerPoint,
  type HistoryRange,
} from '../api/client';
import LogPanel from './LogPanel';

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Filler, Tooltip, Legend);

const RANGES: HistoryRange[] = ['1h', '6h', '24h', '7d'];

const RANGE_MS: Record<HistoryRange, number> = {
  '1h': 60 * 60 * 1000,
  '6h': 6 * 60 * 60 * 1000,
  '24h': 24 * 60 * 60 * 1000,
  '7d': 7 * 24 * 60 * 60 * 1000,
};

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

function fmtTick(ms: number, range: HistoryRange): string {
  const d = new Date(ms);
  if (range === '7d') return d.toLocaleDateString([], { month: 'short', day: 'numeric' });
  if (range === '24h')
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function fmtTooltipTime(ms: number, range: HistoryRange): string {
  const d = new Date(ms);
  if (range === '7d')
    return d.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
  return d.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
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

export default function AgentDetail({
  agent,
  token,
  onBack,
  onExpired,
}: {
  agent: Agent;
  token: string;
  onBack: () => void;
  onExpired: () => void;
}) {
  const [containers, setContainers] = useState<Container[]>([]);
  const [selected, setSelected] = useState<string | null>(null);
  const [range, setRange] = useState<HistoryRange>('1h');
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
      const data = await getContainerHistory(token, agent.id, selected, range);
      setHistory(data.points ?? []);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') onExpired();
    } finally {
      setLoadingChart(false);
    }
  }, [token, agent.id, selected, range, onExpired]);

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
  const rangeMs = RANGE_MS[range];
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
        callback: (v: string | number) => fmtTick(Number(v), range),
      },
      grid: { color: 'rgba(128,128,128,0.06)' },
      border: { display: false },
    }),
    [xMin, xMax, range],
  );

  const cpuChart = useMemo(
    () => ({
      data: {
        datasets: [
          {
            label: cpuInCores ? 'CPU (Core)' : 'CPU (mCore)',
            data: cpuPoints,
            parsing: false as const,
            borderColor: '#3b82f6',
            backgroundColor: 'rgba(59,130,246,0.12)',
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
                return x == null ? '' : fmtTooltipTime(x, range);
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
    [cpuPoints, cpuInCores, sharedXScale, range],
  );

  const memChart = useMemo(
    () => ({
      data: {
        datasets: [
          {
            label: memInGB ? 'RAM (GB)' : 'RAM (MB)',
            data: memPoints,
            parsing: false as const,
            borderColor: '#8b5cf6',
            backgroundColor: 'rgba(139,92,246,0.12)',
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
                return x == null ? '' : fmtTooltipTime(x, range);
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
    [memPoints, memInGB, sharedXScale, range],
  );

  const toggleSelect = useCallback(
    (name: string) => setSelected(prev => (prev === name ? null : name)),
    [],
  );

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
    <div className="dashboard">
      <header className="dash-header">
        <div className="dash-brand">
          <button className="btn-ghost" onClick={onBack} style={{ marginRight: 8 }} aria-label="Back to agents">
            ← Back
          </button>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
          </svg>
          <span className="dash-title">{agent.hostname}</span>
          <span className="agent-id" style={{ marginLeft: 8 }}>{agent.id}</span>
        </div>
      </header>

      <main className="dash-main">
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
                          <span className={`ctr-status ${c.status.toLowerCase().startsWith('up') ? 'up' : 'down'}`}>
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
              <div className="range-tabs" role="tablist" aria-label="Time range">
                {RANGES.map(r => (
                  <button
                    key={r}
                    type="button"
                    role="tab"
                    aria-selected={range === r}
                    className={`range-tab ${range === r ? 'active' : ''}`}
                    onClick={() => setRange(r)}
                  >
                    {r}
                  </button>
                ))}
              </div>
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
      </main>
    </div>
  );
}
