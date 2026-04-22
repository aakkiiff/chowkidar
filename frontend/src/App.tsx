import { useState, useEffect } from 'react';
import Login from './pages/Login';
import Dashboard from './pages/Dashboard';
import { getToken, me } from './api/client';

export default function App() {
  const [username, setUsername] = useState<string | null>(null);
  const [checking, setChecking] = useState(true);

  useEffect(() => {
    const token = getToken();
    if (token) {
      me(token)
        .then(res => {
          setUsername(res.username);
        })
        .catch(() => {
          setUsername(null);
        })
        .finally(() => setChecking(false));
    } else {
      setChecking(false);
    }
  }, []);

  if (checking) {
    return <div className="loading-screen">Loading...</div>;
  }

  if (!username) {
    return <Login onLogin={setUsername} />;
  }

  return <Dashboard username={username} onLogout={() => setUsername(null)} />;
}
