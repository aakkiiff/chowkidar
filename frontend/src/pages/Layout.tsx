import { Link, Outlet, NavLink, useOutletContext } from 'react-router-dom';
import NotificationCenter from '../components/NotificationCenter';
import type { AuthCtx } from './Protected';

export default function Layout() {
  const ctx = useOutletContext<AuthCtx>();

  return (
    <div className="dashboard">
      <header className="dash-header">
        <Link to="/agents" className="dash-brand dash-brand-btn" aria-label="Chowkidar — home">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
          </svg>
          <span className="dash-title">Chowkidar</span>
        </Link>

        <div className="dash-user">
          {ctx.role === 'admin' && (
            <NotificationCenter token={ctx.token} onExpired={ctx.onLogout} />
          )}
          <nav className="dash-nav" aria-label="Primary">
            <NavLink
              to="/settings"
              className={({ isActive }) => `btn-ghost${isActive ? ' dash-nav-active' : ''}`}
            >
              Settings
            </NavLink>
          </nav>
          <span>{ctx.username}</span>
          <button className="btn-ghost" onClick={ctx.onLogout}>Sign out</button>
        </div>
      </header>

      <Outlet context={ctx} />
    </div>
  );
}
