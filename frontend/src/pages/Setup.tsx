import { useState } from 'react';
import { setupAdmin } from '../api/client';
import LoginScene from '../components/LoginScene';

interface Props {
  onComplete: () => void;
}

export default function Setup({ onComplete }: Props) {
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

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

  return (
    <div className="login-page login-page-scene">
      <LoginScene />
      <form className="login-card" onSubmit={handleSubmit}>
        <div className="login-icon">
          <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
            <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
          </svg>
        </div>
        <h1>Chowkidar</h1>
        <p className="login-subtitle">Create your admin account</p>

        {error && <div className="login-error">{error}</div>}

        <input
          type="password"
          placeholder="Password (min 12 characters)"
          value={password}
          onChange={e => setPassword(e.target.value)}
          autoComplete="new-password"
          required
        />
        <input
          type="password"
          placeholder="Confirm password"
          value={confirm}
          onChange={e => setConfirm(e.target.value)}
          autoComplete="new-password"
          required
        />
        <button type="submit" disabled={loading}>
          {loading ? 'Creating account…' : 'Create admin account'}
        </button>
      </form>
    </div>
  );
}
