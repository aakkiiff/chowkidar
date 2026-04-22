import { useState, useEffect, useCallback } from 'react';
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
  getAgentHistory,
  type Agent,
  type Container,
  type HistoryRange,
  type SystemPoint,
} from '../api/client';

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Filler, Tooltip, Legend);

const RANGES: HistoryRange[] = ['1h', '6h', '24h', '7d'];

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
  const [range, setRange] = useState<HistoryRange>('1h');
  const [history, setHistory] = useState<SystemPoint[]>([]);
  const [containers, setContainers] = useState<Container[]>([]);
  const [loadingChart, setLoadingChart] = useState(true);

  const loadHistory = useCallback(async () => {
    setLoadingChart(true);
    try {
      const data = await getAgentHistory(token, agent.id, range);
      setHistory(data.system ?? []);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') onExpired();
    } finally {
      setLoadingChart(false);
    }
  }, [token, agent.id, range, onExpired]);

  const loadContainers = useCallback(async () => {
    try {
      const data = await getAgentContainers(token, agent.id);
      setContainers(data ?? []);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') onExpired();
    }
  }, [token, agent.id, onExpired]);

  useEffect(() => { loadHistory(); }, [loadHistory]);

  useEffect(() => {
    loadContainers();
    const id = setInterval(loadContainers, 10_000);
    return () => clearInterval(id);
  }, [loadContainers]);

  // Build chart datasets: CPU% and Mem% on the same 0-100 scale.
  const labels = history.map(p => fmtLabel(p.timestamp, range));
  const chartData = {
    labels,
    datasets: [
      {
        label: 'CPU %',
        data: history.map(p => +p.cpu_percent.toFixed(2)),
        borderColor: '#3b82f6',
        backgroundColor: 'rgba(59,130,246,0.08)',
        fill: true,
        tension: 0.3,
        pointRadius: 0,
        borderWidth: 1.5,
      },
      {
        label: 'Mem %',
        data: history.map(p =>
          p.mem_total_gb > 0 ? +(p.mem_used_gb / p.mem_total_gb * 100).toFixed(2) : 0,
        ),
        borderColor: '#8b5cf6',
        backgroundColor: 'rgba(139,92,246,0.08)',
        fill: true,
        tension: 0.3,
        pointRadius: 0,
        borderWidth: 1.5,
      },
    ],
  };

  const chartOptions = {
    responsive: true,
    maintainAspectRatio: false,
    animation: { duration: 0 } as const,
    scales: {
      y: {
        min: 0,
        max: 100,
        ticks: {
          callback: (v: string | number) => `${v}%`,
          maxTicksLimit: 5,
        },
        grid: { color: 'rgba(128,128,128,0.1)' },
      },
      x: {
        ticks: { maxTicksLimit: 8, maxRotation: 0 },
        grid: { display: false },
      },
    },
    plugins: {
      legend: { position: 'top' as const, labels: { boxWidth: 12, padding: 16 } },
      tooltip: {
        callbacks: {
          label: (ctx: { dataset: { label?: string }; raw: unknown }) =>
            `${ctx.dataset.label}: ${(ctx.raw as number).toFixed(1)}%`,
        },
      },
    },
  };

  return (
    <div className="dashboard">
      <header className="dash-header">
        <div className="dash-brand">
          <button className="btn-ghost" onClick={onBack} style={{ marginRight: 8 }}>
            ← Back
          </button>
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
            <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
          </svg>
          <span className="dash-title">{agent.hostname}</span>
          <span className="agent-id" style={{ marginLeft: 8 }}>{agent.id}</span>
        </div>
      </header>

      <main className="dash-main">
        {/* System metrics chart */}
        <div className="detail-section">
          <div className="chart-header">
            <span className="detail-section-title">CPU &amp; Memory</span>
            <div className="range-tabs">
              {RANGES.map(r => (
                <button
                  key={r}
                  className={`range-tab ${range === r ? 'active' : ''}`}
                  onClick={() => setRange(r)}
                >
                  {r}
                </button>
              ))}
            </div>
          </div>

          <div className="chart-wrap">
            {loadingChart ? (
              <div className="chart-placeholder">Loading…</div>
            ) : history.length === 0 ? (
              <div className="chart-placeholder">
                No data yet — 1-minute averages appear after the first rollup cycle.
              </div>
            ) : (
              <Line data={chartData} options={chartOptions} />
            )}
          </div>
        </div>

        {/* Container table — live, like docker stats */}
        <div className="detail-section">
          <div className="chart-header">
            <span className="detail-section-title">Containers</span>
            <span className="detail-subtitle">{containers.length} running · refreshes every 10 s</span>
          </div>

          {containers.length === 0 ? (
            <div className="dash-empty">No container data yet.</div>
          ) : (
            <div className="table-wrap">
              <table className="container-table">
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Image</th>
                    <th>CPU</th>
                    <th>Memory</th>
                    <th>Status</th>
                  </tr>
                </thead>
                <tbody>
                  {containers.map(c => (
                    <tr key={c.id}>
                      <td className="td-name">{c.name}</td>
                      <td className="td-muted">{c.image}</td>
                      <td>
                        <span className={`cpu-badge ${c.cpu_percent >= 80 ? 'high' : c.cpu_percent >= 40 ? 'mid' : ''}`}>
                          {c.cpu_percent.toFixed(1)}%
                        </span>
                      </td>
                      <td className="td-muted">{fmtMem(c.mem_used_mb, c.mem_limit_mb)}</td>
                      <td>
                        <span className={`ctr-status ${c.status.toLowerCase().startsWith('up') ? 'up' : 'down'}`}>
                          {c.status}
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </main>
    </div>
  );
}

function fmtLabel(ts: string, range: HistoryRange): string {
  const d = new Date(ts);
  if (range === '7d')
    return d.toLocaleDateString([], { month: 'short', day: 'numeric' });
  if (range === '24h')
    return d.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function fmtMem(used: number, limit: number): string {
  const fmt = (mb: number) => mb >= 1024 ? `${(mb / 1024).toFixed(1)} GB` : `${mb.toFixed(0)} MB`;
  return `${fmt(used)} / ${fmt(limit)}`;
}
