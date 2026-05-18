import { useEffect, useRef, useState } from 'react';
import { setupAdmin } from '../api/client';
import LoginScene from '../components/LoginScene';

interface Props {
  onComplete: () => void;
}

// Password strength heuristic — purely for visual feedback. Server enforces
// min 12 char + 72 char bcrypt cap; the UI just gives the user a sense of
// how strong their pick is before submitting.
function strength(pw: string): { score: 0 | 1 | 2 | 3 | 4; label: string } {
  if (pw.length === 0) return { score: 0, label: '' };
  if (pw.length < 12)  return { score: 1, label: 'Too short' };
  let s = 1;
  if (/[a-z]/.test(pw) && /[A-Z]/.test(pw)) s++;
  if (/\d/.test(pw)) s++;
  if (/[^A-Za-z0-9]/.test(pw)) s++;
  const labels = ['', 'Weak', 'Fair', 'Good', 'Strong'] as const;
  return { score: Math.min(s, 4) as 0 | 1 | 2 | 3 | 4, label: labels[Math.min(s, 4)] };
}

export default function Setup({ onComplete }: Props) {
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const pwRef = useRef<HTMLInputElement>(null);
  useEffect(() => { pwRef.current?.focus(); }, []);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    if (password !== confirm) {
      setError('Passwords do not match');
      return;
    }
    if (password.length < 12) {
      setError('Password must be at least 12 characters');
      return;
    }
    setLoading(true);
    try {
      await setupAdmin(password);
      onComplete();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Setup failed');
    } finally {
      setLoading(false);
    }
  };

  const s = strength(password);
  const mismatch = confirm.length > 0 && confirm !== password;

  return (
    <div className="login-page login-page-scene">
      <LoginScene />
      <form className="login-card" onSubmit={handleSubmit} noValidate>
        <header className="login-head">
          <div className="login-icon">
            <img src="/favicon.svg" width="36" height="36" alt="" aria-hidden="true" />
          </div>
          <h1>Welcome to Chowkidar</h1>
          <p className="login-subtitle">First-time setup — create your admin account</p>
        </header>

        {error && (
          <div className="login-error" role="alert" aria-live="polite">
            <AlertIcon />
            <span>{error}</span>
          </div>
        )}

        <div className="login-field">
          <label htmlFor="setup-pw">Password</label>
          <div className="login-input-wrap">
            <input
              id="setup-pw"
              ref={pwRef}
              type={showPassword ? 'text' : 'password'}
              value={password}
              onChange={e => setPassword(e.target.value)}
              autoComplete="new-password"
              required
              disabled={loading}
              placeholder="At least 12 characters"
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
          {password && (
            <div className="login-strength" data-score={s.score}>
              <div className="login-strength-bar"><span style={{ width: `${(s.score / 4) * 100}%` }} /></div>
              <span className="login-strength-label">{s.label}</span>
            </div>
          )}
        </div>

        <div className="login-field">
          <label htmlFor="setup-confirm">Confirm password</label>
          <input
            id="setup-confirm"
            type={showPassword ? 'text' : 'password'}
            value={confirm}
            onChange={e => setConfirm(e.target.value)}
            autoComplete="new-password"
            required
            disabled={loading}
            placeholder="Re-enter the same password"
            aria-invalid={mismatch || undefined}
          />
          {mismatch && <span className="login-hint err">Passwords don't match yet</span>}
        </div>

        <button
          type="submit"
          className="login-submit"
          disabled={loading || password.length < 12 || password !== confirm}
        >
          {loading ? (<><Spinner /> Creating account…</>) : 'Create admin account'}
        </button>

        <p className="login-foot">
          This account becomes the cluster administrator. Choose a strong password.
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
