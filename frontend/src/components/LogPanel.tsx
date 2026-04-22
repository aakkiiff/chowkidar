import { useEffect, useRef, useState, useCallback, useMemo, memo } from 'react';
import { getRecentLogs, streamLogs, type LogLine } from '../api/client';

const MAX_LINES = 5000;
const FLASH_MS = 4000;

const PRESETS = [5, 15, 30, 60] as const;

const ESC_RE = /[.*+?^${}()|[\]\\]/g;
function escapeRegex(s: string): string {
  return s.replace(ESC_RE, '\\$&');
}

// Splits text into [plain, match, plain, match, …] segments for safe React
// rendering without dangerouslySetInnerHTML. Case-insensitive.
function highlight(text: string, query: string): (string | { match: string })[] {
  if (!query) return [text];
  const re = new RegExp(escapeRegex(query), 'ig');
  const parts: (string | { match: string })[] = [];
  let lastIdx = 0;
  for (let m = re.exec(text); m; m = re.exec(text)) {
    if (m.index > lastIdx) parts.push(text.slice(lastIdx, m.index));
    parts.push({ match: m[0] });
    lastIdx = m.index + m[0].length;
    if (m[0].length === 0) re.lastIndex++; // avoid infinite loop
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
  // Wall-clock ms when the entry arrived in the browser. Used to drive the
  // 4-second flash fade on newly-arrived live lines.
  arrivedAt: number;
}

interface Props {
  token: string;
  agentId: string;
  containerName: string;
  onExpired: () => void;
}

function LogPanelImpl({ token, agentId, containerName, onExpired }: Props) {
  const [entries, setEntries] = useState<Entry[]>([]); // newest-first
  const [live, setLive] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [query, setQuery] = useState('');

  // minutes for the range selector. customMinutes used when isCustom.
  const [minutes, setMinutes] = useState<number>(5);
  const [customMinutes, setCustomMinutes] = useState<string>('');
  const [isCustom, setIsCustom] = useState(false);

  const [flashTick, setFlashTick] = useState(0); // drives flash-fade cleanup renders

  // Monotonic sequence so each entry has a stable unique key even when many
  // lines share the exact same nanosecond timestamp.
  const seqRef = useRef(0);
  const nextId = () => `e${++seqRef.current}`;

  const effectiveMinutes = useMemo(() => {
    if (!isCustom) return minutes;
    const n = parseInt(customMinutes, 10);
    return Number.isFinite(n) && n > 0 ? n : 5;
  }, [isCustom, customMinutes, minutes]);

  // Reset on container change.
  useEffect(() => {
    setEntries([]);
    setError(null);
    setLive(false);
  }, [containerName]);

  // Historical pull (runs when not live, and on Refresh).
  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const rows = await getRecentLogs(token, agentId, containerName, effectiveMinutes);
      // Server returns oldest→newest; store newest-first.
      const next: Entry[] = [];
      for (let i = rows.length - 1; i >= 0; i--) {
        next.push({ ...rows[i], id: nextId(), arrivedAt: 0 });
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
  }, [token, agentId, containerName, effectiveMinutes, onExpired]);

  // Initial pull + pull on range change when not live.
  useEffect(() => {
    if (live) return;
    refresh();
  }, [live, refresh]);

  // Live SSE lifecycle.
  useEffect(() => {
    if (!live) return;

    const ctrl = streamLogs(
      token,
      agentId,
      containerName,
      500,
      line => {
        setEntries(prev => {
          const e: Entry = { ...line, id: nextId(), arrivedAt: Date.now() };
          const next = [e, ...prev];
          return next.length > MAX_LINES ? next.slice(0, MAX_LINES) : next;
        });
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

  // Flash-fade bookkeeping: while any entry is within its flash window,
  // re-render every 500 ms so the class drops off cleanly at 4 s.
  useEffect(() => {
    const hasFresh = entries.some(e => e.arrivedAt > 0 && Date.now() - e.arrivedAt < FLASH_MS);
    if (!hasFresh) return;
    const id = setInterval(() => setFlashTick(t => t + 1), 500);
    return () => clearInterval(id);
  }, [entries]);

  const visible = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return entries;
    return entries.filter(e => e.text.toLowerCase().includes(q));
  }, [entries, query]);

  const toggleLive = useCallback(() => setLive(v => !v), []);

  return (
    <div className="detail-section">
      <div className="log-toolbar">
        <div className="log-toolbar-left">
          <span className="detail-section-title">Logs</span>
          <span className="detail-subtitle">
            {visible.length}{visible.length !== entries.length ? ` / ${entries.length}` : ''} lines
            {live && <span className="log-live-dot" aria-hidden="true" />}
          </span>
        </div>

        <div className="log-controls">
          <label className={`log-range-label${live ? ' log-range-disabled' : ''}`}>
            Last
            <select
              className="form-input log-range-select"
              value={isCustom ? 'custom' : String(minutes)}
              disabled={live}
              onChange={e => {
                const v = e.target.value;
                if (v === 'custom') {
                  setIsCustom(true);
                } else {
                  setIsCustom(false);
                  setMinutes(parseInt(v, 10));
                }
              }}
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
                value={customMinutes}
                disabled={live}
                onChange={e => setCustomMinutes(e.target.value)}
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
          <div className="log-empty">
            {loading ? 'Loading…' : entries.length === 0 ? 'No logs in this range.' : 'No lines match the filter.'}
          </div>
        ) : (
          visible.map(e => {
            const sev = severityOf(e);
            const fresh = e.arrivedAt > 0 && Date.now() - e.arrivedAt < FLASH_MS;
            return (
              <div
                key={e.id}
                className={`log-line log-sev-${sev} log-${e.stream}${fresh ? ' log-new' : ''}`}
                // flashTick is read here only so eslint/react know we depend on it
                data-t={flashTick}
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
