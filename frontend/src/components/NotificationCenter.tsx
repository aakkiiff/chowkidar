import { useCallback, useEffect, useRef, useState } from 'react';
import { streamAlerts, type AlertEvent } from '../api/client';

const MAX_ITEMS = 100;

interface NotificationItem {
  uid: string;
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

// renderValue prints "value vs threshold" with units appropriate to the metric.
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

  useEffect(() => {
    const ctrl = streamAlerts(
      token,
      (evt: AlertEvent) => {
        // Log every alert event the server emits — observed (first detection),
        // fired (sustained past the window, webhook dispatched), and resolved.
        const phase = evt.phase ?? (evt.resolved ? 'resolved' : 'fired');
        const kind: NotificationItem['kind'] =
          phase === 'resolved' ? 'resolved' :
          phase === 'observed' ? 'observed' : 'breach';

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
        setUnread(u => u + 1);
      },
      err => {
        if (err instanceof Error && err.message === 'Session expired') {
          onExpired();
        }
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

  const markSeen = useCallback(() => setUnread(0), []);
  const clearAll = useCallback(() => { setItems([]); setUnread(0); }, []);

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
