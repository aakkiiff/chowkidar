import { useState, useRef, useEffect } from 'react';
import { Link, Outlet, useOutletContext, useNavigate } from 'react-router-dom';
import NotificationCenter from '../components/NotificationCenter';
import type { AuthCtx } from './Protected';

function UserMenu({ username, role, onLogout }: { username: string; role: string; onLogout: () => void }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const navigate = useNavigate();

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  return (
    <div ref={ref} className="user-menu-wrap">
      <button
        type="button"
        className={`user-menu-btn${open ? ' open' : ''}`}
        onClick={() => setOpen(o => !o)}
        aria-haspopup="menu"
        aria-expanded={open}
      >
        <span className="user-name">{username}</span>
        <span className="user-chevron">▾</span>
      </button>
      {open && (
        <div className="user-menu-dropdown" role="menu">
          <div className="user-menu-header">
            <span className="user-menu-username">{username}</span>
            <span className="user-menu-role">{role}</span>
          </div>
          <div className="user-menu-divider" />
          <button
            type="button"
            className="user-menu-item"
            role="menuitem"
            onClick={() => { setOpen(false); navigate('/settings'); }}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/>
              <circle cx="12" cy="12" r="3"/>
            </svg>
            Settings
          </button>
          <div className="user-menu-divider" />
          <button
            type="button"
            className="user-menu-item user-menu-item-danger"
            role="menuitem"
            onClick={() => { setOpen(false); onLogout(); }}
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/>
              <polyline points="16 17 21 12 16 7"/>
              <line x1="21" y1="12" x2="9" y2="12"/>
            </svg>
            Sign out
          </button>
        </div>
      )}
    </div>
  );
}

export default function Layout() {
  const ctx = useOutletContext<AuthCtx>();

  return (
    <div className="dashboard">
      <header className="dash-header">
        <Link to="/agents" className="dash-brand dash-brand-btn" aria-label="Chowkidar — home">
          <img src="/favicon.svg" width="22" height="22" alt="" aria-hidden="true" />
          <span className="dash-title">Chowkidar</span>
        </Link>

        <div className="dash-user">
          {ctx.role === 'admin' && (
            <NotificationCenter token={ctx.token} onExpired={ctx.onLogout} />
          )}
          <UserMenu username={ctx.username} role={ctx.role} onLogout={ctx.onLogout} />
        </div>
      </header>

      <Outlet context={ctx} />
    </div>
  );
}
