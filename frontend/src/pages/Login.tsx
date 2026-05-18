import { useState, useEffect, useRef } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { getToken, login, saveSession } from '../api/client';
import LoginScene from '../components/LoginScene';

interface LocationState {
  from?: { pathname: string };
}

export default function Login() {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [capsOn, setCapsOn] = useState(false);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const navigate = useNavigate();
  const location = useLocation();
  const from = (location.state as LocationState | null)?.from?.pathname || '/agents';
  const usernameRef = useRef<HTMLInputElement>(null);

  // Auto-focus the first field on mount — standard for login pages.
  useEffect(() => { usernameRef.current?.focus(); }, []);

  // If the user is already authenticated, bounce them straight on.
  useEffect(() => {
    if (getToken()) navigate(from, { replace: true });
  }, [navigate, from]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      const res = await login(username, password);
      saveSession(res.username, res.role);
      navigate(from, { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Login failed');
    } finally {
      setLoading(false);
    }
  };

  const handleKey = (e: React.KeyboardEvent<HTMLInputElement>) => {
    setCapsOn(e.getModifierState && e.getModifierState('CapsLock'));
  };

  return (
    <div className="login-page login-page-scene">
      <LoginScene />
      <form className="login-card" onSubmit={handleSubmit} noValidate>
        <header className="login-head">
          <div className="login-icon">
            <img src="/favicon.svg" width="36" height="36" alt="" aria-hidden="true" />
          </div>
          <h1>Chowkidar</h1>
          <p className="login-subtitle">Sign in to your dashboard</p>
        </header>

        {error && (
          <div className="login-error" role="alert" aria-live="polite">
            <AlertIcon />
            <span>{error}</span>
          </div>
        )}

        <div className="login-field">
          <label htmlFor="login-username">Username</label>
          <input
            id="login-username"
            ref={usernameRef}
            type="text"
            value={username}
            onChange={e => setUsername(e.target.value)}
            autoComplete="username"
            autoCapitalize="off"
            autoCorrect="off"
            spellCheck={false}
            required
            disabled={loading}
            placeholder="Your username"
          />
        </div>

        <div className="login-field">
          <div className="login-field-label-row">
            <label htmlFor="login-password">Password</label>
            {capsOn && (
              <span className="login-caps" aria-live="polite">
                <CapsIcon /> Caps Lock is on
              </span>
            )}
          </div>
          <div className="login-input-wrap">
            <input
              id="login-password"
              type={showPassword ? 'text' : 'password'}
              value={password}
              onChange={e => setPassword(e.target.value)}
              onKeyDown={handleKey}
              onKeyUp={handleKey}
              autoComplete="current-password"
              required
              disabled={loading}
              placeholder="••••••••"
            />
            <button
              type="button"
              className="login-input-action"
              onClick={() => setShowPassword(v => !v)}
              aria-label={showPassword ? 'Hide password' : 'Show password'}
              tabIndex={-1}
            >
              {showPassword ? <EyeOffIcon /> : <EyeIcon />}
            </button>
          </div>
        </div>

        <button type="submit" className="login-submit" disabled={loading || !username || !password}>
          {loading ? (
            <>
              <Spinner /> Signing in…
            </>
          ) : (
            'Sign in'
          )}
        </button>

        <p className="login-foot">
          Protected by Chowkidar
        </p>
      </form>
    </div>
  );
}

function AlertIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor"
         strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="12" r="10" />
      <line x1="12" y1="8" x2="12" y2="12" />
      <line x1="12" y1="16" x2="12.01" y2="16" />
    </svg>
  );
}

function CapsIcon() {
  return (
    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor"
         strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 4L4 14h5v6h6v-6h5L12 4z" />
    </svg>
  );
}

function EyeIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor"
         strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" />
      <circle cx="12" cy="12" r="3" />
    </svg>
  );
}

function EyeOffIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor"
         strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M17.94 17.94A10.94 10.94 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A10.94 10.94 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24" />
      <line x1="1" y1="1" x2="23" y2="23" />
    </svg>
  );
}

function Spinner() {
  return (
    <svg className="login-spinner" width="14" height="14" viewBox="0 0 24 24"
         fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" aria-hidden="true">
      <path d="M21 12a9 9 0 1 1-6.219-8.56" />
    </svg>
  );
}
