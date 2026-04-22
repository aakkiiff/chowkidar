import { useEffect, useRef, useState, useCallback, useMemo, memo } from 'react';
import { getRecentLogs, streamLogs, type LogLine } from '../api/client';

const MAX_LINES = 5000;
const PRESETS = [5, 15, 30, 60] as const;

const ESC_RE = /[.*+?^${}()|[\]\\]/g;
function escapeRegex(s: string): string {
  return s.replace(ESC_RE, '\\$&');
}

function highlight(text: string, query: string): (string | { match: string })[] {
  if (!query) return [text];
  const re = new RegExp(escapeRegex(query), 'ig');
  const parts: (string | { match: string })[] = [];
  let lastIdx = 0;
  for (let m = re.exec(text); m; m = re.exec(text)) {
    if (m.index > lastIdx) parts.push(text.slice(lastIdx, m.index));
    parts.push({ match: m[0] });
    lastIdx = m.index + m[0].length;
    if (m[0].length === 0) re.lastIndex++;
  }
  if (lastIdx < text.length) parts.push(text.slice(lastIdx));
  return parts;
}

type Severity = 'error' | 'warn' | 'info';

const ERROR_RE = /\b(error|fatal|panic|exception|traceback)\b/i;
const WARN_RE = /\b(warn|warning|deprecat)/i;

function severityOf(l: LogLine): Severity {
  if (l.stream === 'stderr' || ERROR_RE.test(l.text)) return 'error';
  if (WARN_RE.test(l.text)) return 'warn';
  return 'info';
}

interface Entry extends LogLine {
  id: string;
  // true while rendered for the first time from the live stream. Drives the
  // 4s CSS flash; flipped false by a one-shot timer so the class drops off.
  fresh: boolean;
}

interface Props {
  token: string;
  agentId: string;
  containerName: string;
  onExpired: () => void;
}

function emptyMessage(loading: boolean, hasEntries: boolean): string {
  if (loading) return 'Loading…';
  if (!hasEntries) return 'No logs in this range.';
  return 'No lines match the filter.';
}

function LogPanelImpl({ token, agentId, containerName, onExpired }: Props) {
  const [entries, setEntries] = useState<Entry[]>([]); // newest-first
  const [live, setLive] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState('');
  // `minutes` carries both preset and custom values; "custom" in the UI is
  // simply any minutes value that isn't in PRESETS.
  const [minutes, setMinutes] = useState<number>(5);

  const seqRef = useRef(0);
  const nextId = () => `e${++seqRef.current}`;

  const isCustom = useMemo(() => !(PRESETS as readonly number[]).includes(minutes), [minutes]);

  useEffect(() => {
    setEntries([]);
    setError(null);
    setLive(false);
  }, [containerName]);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const rows = await getRecentLogs(token, agentId, containerName, minutes);
      // Server returns oldest→newest; store newest-first.
      const next: Entry[] = [];
      for (let i = rows.length - 1; i >= 0; i--) {
        next.push({ ...rows[i], id: nextId(), fresh: false });
      }
      setEntries(next);
    } catch (err) {
      if (err instanceof Error && err.message === 'Session expired') {
        onExpired();
        return;
      }
      setError(err instanceof Error ? err.message : 'fetch error');
    } finally {
      setLoading(false);
    }
  }, [token, agentId, containerName, minutes, onExpired]);

  useEffect(() => {
    if (live) return;
    refresh();
  }, [live, refresh]);

  useEffect(() => {
    if (!live) return;

    const ctrl = streamLogs(
      token,
      agentId,
      containerName,
      500,
      line => {
        const id = nextId();
        const entry: Entry = { ...line, id, fresh: true };
        setEntries(prev => {
          const next = [entry, ...prev];
          return next.length > MAX_LINES ? next.slice(0, MAX_LINES) : next;
        });
        // Flip off the flash class after the CSS animation ends so the DOM
        // doesn't hold a meaningless class forever. One timeout per line.
        setTimeout(() => {
          setEntries(prev => {
            const i = prev.findIndex(e => e.id === id);
            if (i < 0 || !prev[i].fresh) return prev;
            const copy = prev.slice();
            copy[i] = { ...copy[i], fresh: false };
            return copy;
          });
        }, 4000);
      },
      err => {
        if (err instanceof Error && err.message === 'Session expired') {
          onExpired();
          return;
        }
        setError(err instanceof Error ? err.message : 'stream error');
        setLive(false);
      },
    );

    return () => ctrl.abort();
  }, [live, token, agentId, containerName, onExpired]);

  const visible = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return entries;
    return entries.filter(e => e.text.toLowerCase().includes(q));
  }, [entries, query]);

  const toggleLive = useCallback(() => setLive(v => !v), []);

  const onSelectRange = (v: string) => {
    if (v === 'custom') {
      // Seed a value outside the presets so the custom input shows.
      if (!isCustom) setMinutes(10);
      return;
    }
    setMinutes(parseInt(v, 10));
  };

  const countLabel = visible.length === entries.length
    ? `${entries.length} lines`
    : `${visible.length} / ${entries.length} lines`;

  return (
    <div className="detail-section">
      <div className="log-toolbar">
        <span className="detail-section-title">Logs</span>
        <span className="detail-subtitle">
          {countLabel}
          {live && <span className="log-live-dot" aria-hidden="true" />}
        </span>

        <div className="log-controls">
          <label className={`log-range-label${live ? ' log-range-disabled' : ''}`}>
            Last
            <select
              className="form-input log-range-select"
              value={isCustom ? 'custom' : String(minutes)}
              disabled={live}
              onChange={e => onSelectRange(e.target.value)}
              aria-label="Time range"
              title={live ? 'Time range disabled while live tailing' : undefined}
            >
              {PRESETS.map(m => (
                <option key={m} value={m}>{fmtMinutes(m)}</option>
              ))}
              <option value="custom">Custom…</option>
            </select>
            {isCustom && (
              <input
                type="number"
                min={1}
                max={1440}
                className="form-input log-custom-input"
                placeholder="min"
                value={minutes}
                disabled={live}
                onChange={e => {
                  const n = parseInt(e.target.value, 10);
                  if (Number.isFinite(n) && n > 0) setMinutes(n);
                }}
                aria-label="Custom minutes"
              />
            )}
          </label>

          <input
            type="search"
            className="form-input log-search"
            placeholder="Filter text…"
            value={query}
            onChange={e => setQuery(e.target.value)}
            aria-label="Filter log text"
          />

          <button
            type="button"
            className="btn-secondary"
            onClick={refresh}
            disabled={loading || live}
            aria-label="Refresh logs"
          >
            {loading ? '…' : '⟳ Refresh'}
          </button>

          <button
            type="button"
            className={live ? 'btn-primary' : 'btn-secondary'}
            onClick={toggleLive}
            aria-pressed={live}
            aria-label={live ? 'Stop live tail' : 'Start live tail'}
          >
            {live ? '■ Live' : '▶ Live'}
          </button>
        </div>
      </div>

      {error && <div className="login-error" style={{ marginBottom: 12 }}>{error}</div>}

      <div
        className="log-view"
        role="log"
        aria-live={live ? 'polite' : 'off'}
        aria-label={`Container logs for ${containerName}, newest first`}
      >
        {visible.length === 0 ? (
          <div className="log-empty">{emptyMessage(loading, entries.length > 0)}</div>
        ) : (
          visible.map(e => {
            const sev = severityOf(e);
            return (
              <div
                key={e.id}
                className={`log-line log-sev-${sev} log-${e.stream}${e.fresh ? ' log-new' : ''}`}
              >
                <span className="log-ts" title={e.timestamp}>{fmtTS(e.timestamp)}</span>
                <span className={`log-stream log-${e.stream}`}>{e.stream}</span>
                <span className="log-text">
                  {highlight(e.text, query.trim()).map((part, i) =>
                    typeof part === 'string'
                      ? part
                      : <mark key={i} className="log-match">{part.match}</mark>,
                  )}
                </span>
              </div>
            );
          })
        )}
      </div>
    </div>
  );
}

function fmtTS(ts: string): string {
  const d = new Date(ts);
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

function fmtMinutes(m: number): string {
  if (m < 60) return `${m}m`;
  if (m < 1440) return `${m / 60}h`;
  return `${m / 1440}d`;
}

export default memo(LogPanelImpl);
