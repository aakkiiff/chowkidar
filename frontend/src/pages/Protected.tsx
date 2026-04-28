import { useEffect, useState } from 'react';
import { Navigate, Outlet, useLocation } from 'react-router-dom';
import { clearSession, getToken, me, type Role } from '../api/client';

// AuthCtx is carried through <Outlet context={...}/> so nested pages get
// username + role + logout without prop drilling or a dedicated context provider.
export interface AuthCtx {
  username: string;
  role: Role;
  token: string;
  onLogout: () => void;
}

type Status = 'checking' | 'auth' | 'unauth';

export default function Protected() {
  const [status, setStatus] = useState<Status>('checking');
  const [username, setUsername] = useState('');
  const [role, setRole] = useState<Role>('developer');
  const [token, setToken] = useState('');
  const location = useLocation();

  useEffect(() => {
    const t = getToken();
    if (!t) { setStatus('unauth'); return; }
    me(t)
      .then(r => {
        setUsername(r.username);
        setRole(r.role ?? 'developer');
        setToken(t);
        setStatus('auth');
      })
      .catch(() => { clearSession(); setStatus('unauth'); });
  }, []);

  if (status === 'checking') {
    return <div className="loading-screen">Loading…</div>;
  }
  if (status === 'unauth') {
    return <Navigate to="/login" state={{ from: location }} replace />;
  }

  const ctx: AuthCtx = {
    username,
    role,
    token,
    onLogout: () => { clearSession(); setStatus('unauth'); },
  };
  return <Outlet context={ctx} />;
}
