import { useCallback, useEffect, useRef, useState } from 'react';
import {
  recentAlerts,
  markAlertsSeen as apiMarkSeen,
  clearAlerts as apiClearAlerts,
  streamAlerts,
  type AlertEvent,
  type PersistedAlertEvent,
} from '../api/client';

const MAX_ITEMS = 200;

interface NotificationItem {
  uid: string;
  // id present means server-persisted; absent means observed (transient).
  id?: number;
  kind: 'breach' | 'resolved' | 'observed';
  hostname: string;
  containerName?: string;
  endpointName?: string;
  metric: AlertEvent['metric'];
  value: number;
  threshold: number;
  timestamp: string;
}

function metricLabel(m: AlertEvent['metric']): string {
  switch (m) {
    case 'cpu': return 'CPU';
    case 'memory': return 'Memory';
    case 'disk': return 'Disk';
    case 'container_cpu': return 'Container CPU';
    case 'container_memory': return 'Container memory';
    case 'container_down': return 'Container down';
    case 'endpoint_down': return 'Endpoint';
    case 'ssl_expiring': return 'SSL';
    case 'agent_down': return 'Agent';
    default: return m;
  }
}

function fmtMCore(m: number): string {
  if (m < 1000) return `${m.toFixed(0)} mCore`;
  return `${(m / 1000).toFixed(2)} Core`;
}

function renderValue(n: { metric: AlertEvent['metric']; value: number; threshold: number; kind: 'breach' | 'resolved' | 'observed' }) {
  const op = n.kind === 'resolved' ? '≤' : '>';
  if (n.metric === 'container_down') {
    return n.kind === 'resolved' ? <>running</> : <>not running</>;
  }
  if (n.metric === 'endpoint_down') {
    return n.kind === 'resolved' ? <>healthy</> : <>down</>;
  }
  if (n.metric === 'agent_down') {
    return n.kind === 'resolved' ? <>reporting again</> : <>not reporting</>;
  }
  if (n.metric === 'container_cpu') {
    return <>{fmtMCore(n.value)} {op} {fmtMCore(n.threshold)}</>;
  }
  return <>{n.value.toFixed(1)}% {op} {n.threshold}%</>;
}

function fmtTime(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

// Map a persisted DB row to the UI item shape. `kind` derives from phase:
//   fired      → breach
//   resolved   → resolved
//   observed   → observed
function fromPersisted(p: PersistedAlertEvent): NotificationItem {
  const kind: NotificationItem['kind'] =
    p.phase === 'resolved' ? 'resolved'
      : p.phase === 'observed' ? 'observed'
      : 'breach';
  return {
    uid: `s${p.id}`,
    id: p.id,
    kind,
    hostname: p.hostname,
    containerName: p.container_name,
    endpointName: p.endpoint_name,
    metric: p.metric,
    value: p.value,
    threshold: p.threshold,
    timestamp: p.fired_at,
  };
}

interface Props {
  token: string;
  onExpired: () => void;
}

export default function NotificationCenter({ token, onExpired }: Props) {
  const [items, setItems] = useState<NotificationItem[]>([]);
  const [unread, setUnread] = useState(0);
  const [open, setOpen] = useState(false);

  const seqRef = useRef(0);
  const panelRef = useRef<HTMLDivElement | null>(null);

  // Initial REST seed — gives us history + unread count even before SSE
  // connects. Then SSE `backlog` event will fully refresh the list.
  useEffect(() => {
    let cancelled = false;
    recentAlerts(100)
      .then(res => {
        if (cancelled) return;
        setItems(res.events.map(fromPersisted));
        setUnread(res.unread);
      })
      .catch(err => {
        if (err instanceof Error && err.message === 'Session expired') onExpired();
      });
    return () => { cancelled = true; };
  }, [onExpired]);

  // Live SSE — backlog frames replace the list, alert frames prepend.
  useEffect(() => {
    const ctrl = streamAlerts(
      token,
      (evt: AlertEvent) => {
        const phase = evt.phase ?? (evt.resolved ? 'resolved' : 'fired');
        const kind: NotificationItem['kind'] =
          phase === 'resolved' ? 'resolved'
            : phase === 'observed' ? 'observed'
            : 'breach';

        const item: NotificationItem = {
          uid: `n${++seqRef.current}`,
          kind,
          hostname: evt.hostname,
          containerName: evt.container_name,
          endpointName: evt.endpoint_name,
          metric: evt.metric,
          value: evt.value,
          threshold: evt.threshold,
          timestamp: evt.timestamp,
        };
        setItems(prev => {
          const next = [item, ...prev];
          return next.length > MAX_ITEMS ? next.slice(0, MAX_ITEMS) : next;
        });
        // Observed events are transient (no DB row); don't bump the bell.
        if (kind !== 'observed') setUnread(u => u + 1);
      },
      (backlog: PersistedAlertEvent[]) => {
        // Server-side backlog on (re)connect. Authoritative — replace list.
        setItems(backlog.map(fromPersisted));
        const u = backlog.filter(b => !b.seen_at).length;
        setUnread(u);
      },
      err => {
        if (err instanceof Error && err.message === 'Session expired') onExpired();
      },
    );
    return () => ctrl.abort();
  }, [token, onExpired]);

  // Close on outside click.
  useEffect(() => {
    if (!open) return;
    const onDocClick = (e: MouseEvent) => {
      if (!panelRef.current) return;
      if (!panelRef.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', onDocClick);
    return () => document.removeEventListener('mousedown', onDocClick);
  }, [open]);

  const markSeen = useCallback(async () => {
    setUnread(0);
    try { await apiMarkSeen(); } catch { /* best-effort */ }
  }, []);

  // Clear permanently wipes the persisted alert log on the server. Asks for
  // confirmation because there's no undo. Local state is reset only after the
  // server delete succeeds, so a failed request doesn't desync the UI.
  const clearAll = useCallback(async () => {
    if (!confirm('Permanently delete all notification history? This cannot be undone.')) return;
    try {
      await apiClearAlerts();
      setItems([]);
      setUnread(0);
    } catch (err) {
      alert(err instanceof Error ? err.message : 'Failed to clear notifications');
    }
  }, []);

  return (
    <div className="notif-wrap" ref={panelRef}>
      <button
        type="button"
        className={`notif-bell${unread > 0 ? ' has-unread' : ''}`}
        onClick={() => setOpen(v => !v)}
        aria-label={`Notifications${unread > 0 ? ` (${unread} unread)` : ''}`}
        aria-expanded={open}
      >
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9" />
          <path d="M10.3 21a1.94 1.94 0 0 0 3.4 0" />
        </svg>
        {unread > 0 && <span className="notif-badge" aria-hidden="true">{unread > 99 ? '99+' : unread}</span>}
      </button>

      {open && (
        <div className="notif-panel" role="dialog" aria-label="Notifications">
          <div className="notif-head">
            <span className="notif-head-title">Notifications</span>
            <div className="notif-head-actions">
              <button
                type="button"
                className="notif-action"
                onClick={markSeen}
                disabled={unread === 0}
              >
                Mark as seen
              </button>
              <button
                type="button"
                className="notif-action"
                onClick={clearAll}
                disabled={items.length === 0}
              >
                Clear
              </button>
            </div>
          </div>

          <div className="notif-list">
            {items.length === 0 ? (
              <div className="notif-empty">No notifications yet.</div>
            ) : (
              items.map(n => (
                <div key={n.uid} className={`notif-item notif-${n.kind}`}>
                  <span className="notif-dot" aria-hidden="true" />
                  <div className="notif-body">
                    <div className="notif-title">
                      {metricLabel(n.metric)}
                      {' '}
                      {n.kind === 'resolved' ? 'resolved'
                        : n.kind === 'observed' ? 'observed'
                        : 'breach (notified)'}
                    </div>
                    <div className="notif-text">
                      <strong>{n.hostname}</strong>
                      {n.containerName && <> · <span className="notif-ctr">{n.containerName}</span></>}
                      {n.endpointName && <> · <span className="notif-ctr">{n.endpointName}</span></>}
                      {' · '}
                      {renderValue(n)}
                    </div>
                  </div>
                  <span className="notif-time">{fmtTime(n.timestamp)}</span>
                </div>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  );
}
