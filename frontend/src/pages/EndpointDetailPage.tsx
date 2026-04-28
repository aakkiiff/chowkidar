import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate, useOutletContext, useParams } from 'react-router-dom';
import {
  listEndpoints,
  getEndpointIncidents,
  getEndpointUptime,
  getEndpointProbes,
  type Endpoint,
  type EndpointIncident,
  type UptimeStats,
  type EndpointProbe,
} from '../api/client';
import type { AuthCtx } from './Protected';

const RANGES: { id: string; label: string; minutes: number }[] = [
  { id: '1h',  label: '1h',  minutes: 60 },
  { id: '6h',  label: '6h',  minutes: 6 * 60 },
  { id: '12h', label: '12h', minutes: 12 * 60 },
  { id: '24h', label: '24h', minutes: 24 * 60 },
  { id: '7d',  label: '7d',  minutes: 7 * 24 * 60 },
];

const REFRESH_MS = 15_000;

function fmtDuration(secs: number): string {
  if (secs < 1) return '<1s';
  if (secs < 60) return `${Math.round(secs)}s`;
  if (secs < 3600) {
    const m = Math.floor(secs / 60);
    const s = Math.round(secs % 60);
    return s ? `${m}m ${s}s` : `${m}m`;
  }
  if (secs < 86400) {
    const h = Math.floor(secs / 3600);
    const m = Math.round((secs % 3600) / 60);
    return m ? `${h}h ${m}m` : `${h}h`;
  }
  return `${Math.floor(secs / 86400)}d ${Math.floor((secs % 86400) / 3600)}h`;
}

function fmtClock(iso: string): string {
  return new Date(iso).toLocaleString();
}

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

export default function EndpointDetailPage() {
  const { id: agentId = '', eid = '' } = useParams<{ id: string; eid: string }>();
  const endpointId = Number(eid);
  const { token, onLogout } = useOutletContext<AuthCtx>();
  const navigate = useNavigate();

  const [endpoint, setEndpoint] = useState<Endpoint | null>(null);
  const [range, setRange] = useState<string>('24h');
  const [incidents, setIncidents] = useState<EndpointIncident[]>([]);
  const [stats, setStats] = useState<UptimeStats | null>(null);
  const [probes, setProbes] = useState<EndpointProbe[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const rangeMinutes = useMemo(
    () => RANGES.find(r => r.id === range)?.minutes ?? RANGES[0].minutes,
    [range],
  );

  const load = useCallback(async () => {
    try {
      const [list, incs, st, recentProbes] = await Promise.all([
        listEndpoints(token, agentId),
        getEndpointIncidents(token, endpointId, range),
        getEndpointUptime(token, endpointId, range),
        getEndpointProbes(token, endpointId, 60).catch(() => [] as EndpointProbe[]),
      ]);
      setEndpoint((list ?? []).find(e => e.id === endpointId) ?? null);
      setIncidents(incs ?? []);
      setStats(st);
      setProbes(recentProbes ?? []);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') { onLogout(); return; }
      setError(err instanceof Error ? err.message : 'load failed');
    } finally {
      setLoading(false);
    }
  }, [token, agentId, endpointId, range, onLogout]);

  useEffect(() => {
    load();
    const id = setInterval(load, REFRESH_MS);
    return () => clearInterval(id);
  }, [load]);

  if (loading) return <div className="dash-loading" style={{ padding: 48 }}>Loading endpoint…</div>;
  if (error || !endpoint) {
    return (
      <main className="dash-main">
        <div className="dash-section">
          <div className="dash-empty">{error ?? 'Endpoint not found.'}</div>
          <button className="btn-secondary" style={{ marginTop: 16 }} onClick={() => navigate(`/agents/${agentId}/endpoints`)}>
            ← Back to endpoints
          </button>
        </div>
      </main>
    );
  }

  return (
    <main className="dash-main">
      <div className="detail-section">
        <div className="chart-header">
          <div>
            <span className="detail-section-title">{endpoint.name}</span>
            <div className="detail-subtitle" style={{ marginTop: 6 }}>
              <a href={endpoint.url} target="_blank" rel="noreferrer" className="endpoint-url">
                {endpoint.url}
              </a>
            </div>
          </div>
          <button
            type="button"
            className="btn-secondary"
            onClick={() => navigate(`/agents/${agentId}/endpoints`)}
          >
            ← All endpoints
          </button>
        </div>

        {/* KPI strip */}
        <div className="kpi-strip">
          <div className="kpi-tile">
            <span className="kpi-label">Uptime</span>
            <span className="kpi-val mono">{stats ? `${stats.percent.toFixed(2)}%` : '—'}</span>
          </div>
          <div className="kpi-tile">
            <span className="kpi-label">Total downtime</span>
            <span className="kpi-val mono">{stats ? fmtDuration(stats.down_seconds) : '—'}</span>
          </div>
          <div className="kpi-tile">
            <span className="kpi-label">Incidents</span>
            <span className="kpi-val mono">{stats?.incident_count ?? 0}</span>
          </div>
          <div className="kpi-tile">
            <span className="kpi-label">Longest outage</span>
            <span className="kpi-val mono">{stats && stats.longest_seconds > 0 ? fmtDuration(stats.longest_seconds) : '—'}</span>
          </div>
          {(() => {
            const cert = fmtCertAge(endpoint.last_cert_not_after);
            if (!cert) return null;
            return (
              <div className="kpi-tile">
                <span className="kpi-label">SSL</span>
                <span
                  className={`kpi-val mono ${cert.className}`}
                  title={endpoint.last_cert_not_after
                    ? `expires ${new Date(endpoint.last_cert_not_after).toLocaleString()}`
                    : ''}
                >
                  {cert.text}
                </span>
              </div>
            );
          })()}
        </div>

        {/* Range tabs */}
        <div className="range-tabs" role="tablist" aria-label="Uptime range" style={{ marginBottom: 12 }}>
          {RANGES.map(r => (
            <button
              key={r.id}
              type="button"
              role="tab"
              aria-selected={range === r.id}
              className={`range-tab ${range === r.id ? 'active' : ''}`}
              onClick={() => setRange(r.id)}
            >
              {r.label}
            </button>
          ))}
        </div>

        <Gantt rangeMinutes={rangeMinutes} incidents={incidents} />
      </div>

      <div className="detail-section">
        <div className="chart-header">
          <span className="detail-section-title">Incidents</span>
          <span className="detail-subtitle">
            {incidents.length} in {range} window
          </span>
        </div>

        {incidents.length === 0 ? (
          <div className="dash-empty">No outages in this window.</div>
        ) : (
          <div className="table-wrap">
            <table className="container-table">
              <thead>
                <tr>
                  <th scope="col">Started</th>
                  <th scope="col">Ended</th>
                  <th scope="col">Duration</th>
                  <th scope="col">Status</th>
                  <th scope="col">Probes</th>
                  <th scope="col">Last error</th>
                </tr>
              </thead>
              <tbody>
                {incidents.map(inc => (
                  <tr key={inc.id}>
                    <td className="td-name mono">{fmtClock(inc.started_at)}</td>
                    <td className="td-muted mono">{inc.ended_at ? fmtClock(inc.ended_at) : <span className="ctr-status down">ongoing</span>}</td>
                    <td className="mono">{fmtDuration(inc.duration_s)}</td>
                    <td className="mono">{inc.last_status || '—'}</td>
                    <td className="mono">{inc.probe_count}</td>
                    <td className="td-muted">{inc.last_error || ''}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <div className="detail-section">
        <div className="chart-header">
          <span className="detail-section-title">Latency (last hour)</span>
          <span className="detail-subtitle">{probes.length} probes</span>
        </div>
        {probes.length === 0 ? (
          <div className="dash-empty">No recent probes.</div>
        ) : (
          <LatencyMini probes={probes} />
        )}
      </div>
    </main>
  );
}

// Gantt: horizontal SVG. Green default, red overlay per incident window.
function Gantt({ rangeMinutes, incidents }: { rangeMinutes: number; incidents: EndpointIncident[] }) {
  const now = Date.now();
  const start = now - rangeMinutes * 60_000;
  const end = now;
  const width = 1000; // logical width; scales via viewBox
  const height = 36;

  const segments = incidents.map(inc => {
    const s = new Date(inc.started_at).getTime();
    const e = inc.ended_at ? new Date(inc.ended_at).getTime() : now;
    const clampedS = Math.max(s, start);
    const clampedE = Math.min(e, end);
    if (clampedE <= clampedS) return null;
    const x = ((clampedS - start) / (end - start)) * width;
    const w = Math.max(2, ((clampedE - clampedS) / (end - start)) * width);
    return { id: inc.id, x, w, ongoing: !inc.ended_at, started: inc.started_at, ended: inc.ended_at, status: inc.last_status, probes: inc.probe_count };
  }).filter(Boolean) as Array<{ id: number; x: number; w: number; ongoing: boolean; started: string; ended?: string; status: number; probes: number }>;

  return (
    <div className="gantt-wrap">
      <svg viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none" className="gantt-svg" role="img" aria-label="Uptime gantt">
        {/* base track */}
        <rect x={0} y={8} width={width} height={height - 16} className="gantt-up" rx={2} />
        {segments.map(seg => (
          <rect
            key={seg.id}
            x={seg.x}
            y={6}
            width={seg.w}
            height={height - 12}
            className={`gantt-down ${seg.ongoing ? 'gantt-ongoing' : ''}`}
            rx={2}
          >
            <title>
              {seg.ongoing
                ? `DOWN since ${fmtClock(seg.started)} · ${seg.probes} probes · status ${seg.status || 'err'}`
                : `DOWN ${fmtClock(seg.started)} → ${fmtClock(seg.ended!)} · ${seg.probes} probes · status ${seg.status || 'err'}`}
            </title>
          </rect>
        ))}
      </svg>
      <div className="gantt-axis mono">
        <span>{new Date(start).toLocaleString()}</span>
        <span>now</span>
      </div>
    </div>
  );
}

function fmtLatencyVal(ms: number): string {
  if (ms < 1) return `${ms.toFixed(1)} ms`;
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(2)} s`;
}

function fmtTimeShort(ts: number): string {
  const d = new Date(ts);
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function niceMax(v: number): number {
  if (v <= 0) return 1;
  const pow = Math.pow(10, Math.floor(Math.log10(v)));
  const n = v / pow;
  let nice: number;
  if (n <= 1) nice = 1;
  else if (n <= 2) nice = 2;
  else if (n <= 5) nice = 5;
  else nice = 10;
  return nice * pow;
}

function LatencyMini({ probes }: { probes: EndpointProbe[] }) {
  const sorted = [...probes].sort(
    (a, b) => new Date(a.probed_at).getTime() - new Date(b.probed_at).getTime(),
  );
  if (sorted.length === 0) return null;

  const W = 1000;
  const H = 200;
  const padL = 56;
  const padR = 12;
  const padT = 12;
  const padB = 32;
  const innerW = W - padL - padR;
  const innerH = H - padT - padB;

  const xs = sorted.map(p => new Date(p.probed_at).getTime());
  const ys = sorted.map(p => p.latency_ms);
  const xMin = xs[0];
  const xMax = xs[xs.length - 1];
  const xSpan = Math.max(1, xMax - xMin);
  const rawMax = Math.max(...ys, 1);
  const yMax = niceMax(rawMax * 1.1);

  const sx = (t: number) => padL + ((t - xMin) / xSpan) * innerW;
  const sy = (v: number) => padT + innerH - (v / yMax) * innerH;

  const path = sorted
    .map((_p, i) => `${i === 0 ? 'M' : 'L'}${sx(xs[i]).toFixed(1)},${sy(ys[i]).toFixed(1)}`)
    .join(' ');

  const yTicks = [0, 0.25, 0.5, 0.75, 1].map(f => f * yMax);
  const xTickCount = 5;
  const xTicks = Array.from({ length: xTickCount }, (_, i) =>
    xMin + (xSpan * i) / (xTickCount - 1),
  );

  const okPts = sorted.filter(p => p.ok);
  const failPts = sorted.filter(p => !p.ok);

  return (
    <div className="latency-chart">
      <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className="latency-mini" role="img" aria-label="Latency over time">
        {yTicks.map((v, i) => (
          <g key={`y${i}`}>
            <line
              x1={padL} x2={W - padR}
              y1={sy(v)} y2={sy(v)}
              className="lat-grid"
            />
            <text
              x={padL - 8} y={sy(v) + 4}
              className="lat-tick" textAnchor="end"
            >
              {fmtLatencyVal(v)}
            </text>
          </g>
        ))}
        {xTicks.map((t, i) => (
          <text
            key={`x${i}`}
            x={sx(t)} y={H - padB + 18}
            className="lat-tick"
            textAnchor={i === 0 ? 'start' : i === xTicks.length - 1 ? 'end' : 'middle'}
          >
            {fmtTimeShort(t)}
          </text>
        ))}
        <line
          x1={padL} x2={W - padR}
          y1={padT + innerH} y2={padT + innerH}
          className="lat-axis"
        />
        <line
          x1={padL} x2={padL}
          y1={padT} y2={padT + innerH}
          className="lat-axis"
        />
        <path d={path} className="latency-mini-line" fill="none" />
        {okPts.map(p => (
          <circle
            key={`ok-${p.id}`}
            cx={sx(new Date(p.probed_at).getTime())}
            cy={sy(p.latency_ms)}
            r={2.5}
            className="latency-pt-ok"
          >
            <title>{`${new Date(p.probed_at).toLocaleString()} · ${fmtLatencyVal(p.latency_ms)} · ${p.status_code || 'ok'}`}</title>
          </circle>
        ))}
        {failPts.map(p => (
          <circle
            key={`fail-${p.id}`}
            cx={sx(new Date(p.probed_at).getTime())}
            cy={sy(p.latency_ms)}
            r={3.5}
            className="latency-pt-fail"
          >
            <title>{`${new Date(p.probed_at).toLocaleString()} · ${fmtLatencyVal(p.latency_ms)} · ${p.status_code || 'err'}${p.error ? ' · ' + p.error : ''}`}</title>
          </circle>
        ))}
        <text x={padL} y={padT - 2} className="lat-axis-label">ms</text>
      </svg>
    </div>
  );
}
